// Copyright 2010 Yves Junqueira
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package javaitarde

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/garyburd/go-oauth/oauth"
)

const (
	TWITTER_API_BASE    = "http://api.twitter.com/1"
	TWITTER_GET_TIMEOUT = 10 * time.Second
)

var oauthClient = oauth.Client{
	Credentials:                   oauth.Credentials{clientToken, clientSecret},
	TemporaryCredentialRequestURI: "http://api.twitter.com/oauth/request_token",
	ResourceOwnerAuthorizationURI: "http://api.twitter.com/oauth/authenticate",
	TokenRequestURI:               "http://api.twitter.com/oauth/access_token",
}

type twitterClient struct {
	twitterToken *oauth.Credentials
}

func newTwitterClient() *twitterClient {
	return &twitterClient{twitterToken: &oauth.Credentials{accessToken, accessTokenSecret}}
}

func (tw *twitterClient) twitterGet(url string, param url.Values) (p []byte, err error) {
	oauthClient.SignParam(tw.twitterToken, "GET", url, param)
	url = url + "?" + param.Encode()
	var resp *http.Response
	done := make(chan bool, 1)
	go func() {
		resp, err = http.Get(url)
		done <- true
	}()

	timeout := time.After(TWITTER_GET_TIMEOUT) //
	select {
	case <-done:
		break
	case <-timeout:
		return nil, errors.New("http Get timed out - " + url)
	}
	return readHttpResponse(resp, err)
}

// twitterPost issues a POST query to twitter to the given url, using parameters from param. The params must be URL
// escaped already.
func (tw *twitterClient) twitterPost(url string, param url.Values) (p []byte, err error) {
	oauthClient.SignParam(tw.twitterToken, "POST", url, param)

	// TODO: remove this dupe.
	var resp *http.Response
	done := make(chan bool, 1)
	go func() {
		resp, err = http.PostForm(url, param)
		done <- true
	}()

	timeout := time.After(TWITTER_GET_TIMEOUT) // post in this case.
	select {
	case <-done:
		break
	case <-timeout:
		return nil, errors.New("http POST timed out - " + url)
	}
	return readHttpResponse(resp, err)
}

func (tw *twitterClient) getUserName(uid int64) (screenName string, err error) {
	param := make(url.Values)
	param.Set("id", strconv.FormatInt(uid, 10))
	url := TWITTER_API_BASE + "/users/show.json"

	userDetails := map[string]interface{}{}
	resp, err := tw.twitterGet(url, param)
	if err != nil {
		return
	}
	if err = json.Unmarshal(resp, &userDetails); err != nil {
		return
	}
	screenName = userDetails["screen_name"].(string)
	return
}

type userFollowers struct {
	Uid       int64   `bson:"uid"`
	Date      int64   `bson:"date"`
	Followers []int64 `bson:"followers"`
}

type getFollowersResult struct {
	Ids        []int64 `bson:"ids"`
	NextCursor int64   `bson:"next_cursor"`
}

// getUserFollowers retrieves the followers of a user. If uid != 0, uses the uid for searching, otherwise searches by
// screenName.
func (tw *twitterClient) getUserFollowers(uid int64, screenName string) (uf *userFollowers, err error) {
	param := make(url.Values)
	if uid != 0 {
		param.Set("id", strconv.FormatInt(uid, 10))
	} else {
		param.Set("screen_name", screenName)
	}
	cursor := int64(-1)
	url := TWITTER_API_BASE + "/followers/ids.json"
	var followers []int64
	for {
		param.Set("cursor", strconv.FormatInt(cursor, 10))
		resp, err := tw.twitterGet(url, param)
		if err != nil {
			return nil, err
		}
		var result getFollowersResult

		if err = json.Unmarshal(resp, &result); err != nil {
			log.Println("unmarshal error", err.Error())
			log.Println("output was:", string(resp))
			return nil, err
		}
		if len(result.Ids) == 0 {
			return nil, errors.New("no followers.")
		}
		followers = append(followers, result.Ids...)
		if result.NextCursor == 0 {
			break
			log.Println("done getting followers for", strconv.FormatInt(uid, 10))
		}
		cursor = result.NextCursor
	}
	return &userFollowers{uid, time.Now().UTC().Unix(), followers}, nil
}

func (tw *twitterClient) NotifyUnfollower(abandonedName, unfollowerName string) (err error) {
	// TODO: Should be "sendDirectMessage".
	url_ := TWITTER_API_BASE + "/direct_messages/new.json"
	param := make(url.Values)
	param.Set("screen_name", abandonedName)
	// TODO: translate messages.
	param.Set("text", fmt.Sprintf("Xiiii.. você não está mais sendo seguido por @%s :-(.", unfollowerName))

	p, err := tw.twitterPost(url_, param)
	if err != nil {
		log.Println("notify unfollower error:", err.Error())
		log.Println("response", string(p))
	} else {
		log.Printf("Notified %v of unfollow by %v", abandonedName, unfollowerName)
	}
	return
}

func (tw *twitterClient) FollowUser(uid int64) (err error) {
	url_ := TWITTER_API_BASE + "/friendships/create.json"
	param := make(url.Values)
	param.Set("user_id", strconv.FormatInt(uid, 10))
	param.Set("follow", "true")
	log.Println("Trying to follow user", uid)
	p, err := tw.twitterPost(url_, param)
	if err != nil {
		log.Println("follower user error:", err.Error())
		fmt.Println("response", string(p))
	}
	return
}

func parseResponseError(p []byte) string {
	var r map[string]string
	if err := json.Unmarshal(p, &r); err != nil {
		log.Printf("parseResponseError json.Unmarshal error: %v", err)
		return ""
	}
	e, ok := r["error"]
	if !ok {
		return ""
	}
	return e

}

func readHttpResponse(resp *http.Response, httpErr error) (p []byte, err error) {
	err = httpErr
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if resp == nil {
		err = errors.New("Received null response from http library.")
		log.Println(err)
		return nil, err
	}
	p, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	rateLimit(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		e := parseResponseError(p)
		if e == "" {
			e = "unknown"
		}
		err = errors.New(fmt.Sprintf("Server Error code: %d; msg: %v", resp.StatusCode, e))
		return nil, err
	}
	return p, nil

}

func rateLimit(resp *http.Response) {
	if resp == nil {
		return
	}
	curr := time.Now().UTC()
	r, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	reset := time.Unix(r, 0)
	remaining, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Remaining"), 10, 64)
	if remaining < 1 {
		sleep := reset.Sub(curr)
		if sleep > 0 {
			log.Printf("Twitter API limits exceeded. Sleeping for %v.\n", sleep)
			time.Sleep(sleep)
			return
		}
		log.Printf("Rate limited by twitter but X-RateLimit-Reset is in the past: block should have expired %v ago", time.Since(reset))
	}
}
