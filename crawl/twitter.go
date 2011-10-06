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
	"fmt"
	"http"
	"io/ioutil"
	"json"
	"log"
	"os"
	"strconv"
	"time"
	"url"

	"github.com/garyburd/twister/oauth"
)

const (
	TWITTER_API_BASE    = "http://api.twitter.com/1"
	TWITTER_GET_TIMEOUT = 10 // seconds.
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

func (tw *twitterClient) twitterGet(url string, param url.Values) (p []byte, err os.Error) {
	oauthClient.SignParam(tw.twitterToken, "GET", url, param)
	url = url + "?" + param.Encode()
	var resp *http.Response
	done := make(chan bool, 1)
	go func() {
		resp, err = http.Get(url)
		done <- true
	}()

	timeout := time.After(TWITTER_GET_TIMEOUT * 1e9) //
	select {
	case <-done:
		break
	case <-timeout:
		return nil, os.NewError("http Get timed out - " + url)
	}
	return readHttpResponse(resp, err)
}

// twitterPost issues a POST query to twitter to the given url, using parameters from param. The params must be URL
// escaped already.
func (tw *twitterClient) twitterPost(url string, param url.Values) (p []byte, err os.Error) {
	oauthClient.SignParam(tw.twitterToken, "POST", url, param)

	// TODO: remove this dupe.
	var resp *http.Response
	done := make(chan bool, 1)
	go func() {
		resp, err = http.PostForm(url, param)
		done <- true
	}()

	timeout := time.After(TWITTER_GET_TIMEOUT * 1e9) // post in this case.
	select {
	case <-done:
		break
	case <-timeout:
		return nil, os.NewError("http POST timed out - " + url)
	}
	return readHttpResponse(resp, err)
}

func (tw *twitterClient) getUserName(uid int64) (screenName string, err os.Error) {
	param := make(url.Values)
	param.Set("id", strconv.Itoa64(uid))
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
	Uid       int64   "uid"
	Date      int64   "date"
	Followers []int64 "followers"
}

// getUserFollowers retrieves the followers of a user. If uid != 0, uses the uid for searching, otherwise searches by
// screenName.
func (tw *twitterClient) getUserFollowers(uid int64, screenName string) (uf *userFollowers, err os.Error) {
	param := make(url.Values)
	if uid != 0 {
		param.Set("id", strconv.Itoa64(uid))
	} else {
		param.Set("screen_name", screenName)
	}
	url := TWITTER_API_BASE + "/followers/ids.json"

	resp, err := tw.twitterGet(url, param)
	if err != nil {
		return
	}

	var followers []int64
	if err = json.Unmarshal(resp, &followers); err != nil {
		log.Println("unmarshal error", err.String())
		log.Println("output was:", resp)
		return
	}
	return &userFollowers{uid, time.UTC().Seconds(), followers}, nil
}

func (tw *twitterClient) NotifyUnfollower(abandonedName, unfollowerName string) (err os.Error) {
	// TODO: Should be "sendDirectMessage".
	url_ := TWITTER_API_BASE + "/direct_messages/new.json"
	param := make(url.Values)
	param.Set("screen_name", abandonedName)
	// TODO: translate messages.
	param.Set("text", fmt.Sprintf("Xiiii.. você não está mais sendo seguido por @%s :-(.", unfollowerName))

	p, err := tw.twitterPost(url_, param)
	if err != nil {
		log.Println("notify unfollower error:", err.String())
		log.Println("response", string(p))
	} else {
		log.Printf("Notified %v of unfollow by %v", abandonedName, unfollowerName)
	}
	return
}

func (tw *twitterClient) FollowUser(uid int64) (err os.Error) {
	url_ := TWITTER_API_BASE + "/friendships/create.json"
	param := make(url.Values)
	param.Set("user_id", strconv.Itoa64(uid))
	param.Set("follow", "true")
	log.Println("Trying to follow user", uid)
	p, err := tw.twitterPost(url_, param)
	if err != nil {
		log.Println("follower user error:", err.String())
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

func readHttpResponse(resp *http.Response, httpErr os.Error) (p []byte, err os.Error) {
	err = httpErr
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if resp == nil {
		err = os.NewError("Received null response from http library.")
		return nil, err
	}
	p, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	rateLimitStats(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		e := parseResponseError(p)
		if e == "" {
			e = "unknown"
		}
		err = os.NewError(fmt.Sprintf("Server Error code: %d; msg: %v", resp.StatusCode, e))
		return nil, err
	}
	return p, nil

}

func rateLimitStats(resp *http.Response) {
	if resp == nil {
		return
	}
	curr := time.Seconds()
	reset, _ := strconv.Atoi64(resp.Header.Get("X-RateLimit-Reset"))
	remaining, _ := strconv.Atoi64(resp.Header.Get("X-RateLimit-Remaining"))
	if remaining < 1 && reset-curr > 0 {
		log.Printf("Twitter API limits exceeded. Sleeping for %d seconds.\n", reset-curr)
		time.Sleep((reset - curr) * 1e9)
	}
}
