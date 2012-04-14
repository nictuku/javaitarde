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
	"strings"
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
	return tw.request("GET", url, param)
}

// twitterPost issues a POST query to twitter to the given url, using parameters from param. The params must be URL
// escaped already.
func (tw *twitterClient) twitterPost(url string, param url.Values) (p []byte, err error) {
	return tw.request("POST", url, param)
}

func (tw *twitterClient) request(method string, url string, param url.Values) (p []byte, err error) {
	// I can't use POST for all requests. Certain API methods require GET too.
	oauthClient.SignParam(tw.twitterToken, method, url, param)
	var resp *http.Response
	done := make(chan bool, 1)
	switch method {
	case "POST":
		go func() {
			resp, err = http.PostForm(url, param)
			done <- true
		}()
	case "GET":
		url = url + "?" + param.Encode()
		go func() {
			resp, err = http.Get(url)
			done <- true
		}()
	}

	timeout := time.After(TWITTER_GET_TIMEOUT)
	select {
	case <-done:
		break
	case <-timeout:
		return nil, fmt.Errorf("http %v timed out - %v", method, url)
	}
	return readHttpResponse(resp, err)
}

func (tw *twitterClient) verifyCredentials() error {
	u := TWITTER_API_BASE + "/account/verify_credentials.json"
	_, err := tw.twitterGet(u, make(url.Values))
	return err
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

type NotAuthorizedError struct{}

func (NotAuthorizedError) Error() string {
	return "twitter: Not Authorized"
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

	var (
		followers []int64
		resp      []byte
		url       = TWITTER_API_BASE + "/followers/ids.json"
		cursor    = int64(-1)
		result    getFollowersResult
	)

	for {
		param.Set("cursor", strconv.FormatInt(cursor, 10))
		resp, err = tw.twitterGet(url, param)
		if err != nil {
			if strings.Contains(err.Error(), " 401") {
				err = NotAuthorizedError{}
				return
			}
		}

		if err = json.Unmarshal(resp, &result); err != nil {
			log.Println("unmarshal error", err.Error())
			log.Println("output was:", string(resp))
			return
		}
		if len(result.Ids) == 0 {
			err = errors.New("no followers.")
			return
		}
		followers = append(followers, result.Ids...)
		if result.NextCursor == 0 {
			// Done.
			break
		}
		cursor = result.NextCursor
	}
	uf = &userFollowers{uid, time.Now().UTC().Unix(), followers}
	return
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
	_, err = tw.twitterPost(url_, param)
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

// rateLimit blocks the execution if the request quota with twitter was
// execeeded. It unblocks when the quota is reset.
func rateLimit(resp *http.Response) {
	if resp == nil {
		return
	}
	curr := time.Now().UTC()
	hreset := resp.Header.Get("X-RateLimit-Reset")
	hremaining := resp.Header.Get("X-RateLimit-Remaining")
	if hreset == "" || hremaining == "" {
		return
	}
	remaining, _ := strconv.ParseInt(hremaining, 10, 64)
	r, _ := strconv.ParseInt(hreset, 10, 64)
	reset := time.Unix(r, 0)
	if remaining < 1 && !reset.IsZero() {
		sleep := reset.Sub(curr)
		if sleep > 0 {
			log.Printf("Twitter API limits exceeded. Sleeping for %v.\n", sleep)
			time.Sleep(sleep)
			return
		}
		log.Printf("Rate limited by twitter but X-RateLimit-Reset is in the past: block should have expired %v ago (timestamp: %v)", time.Since(reset), hreset)
	}
}
