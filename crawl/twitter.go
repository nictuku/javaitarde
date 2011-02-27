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
	"github.com/garyburd/twister/oauth"
	"github.com/garyburd/twister/web"
	"http"
	"io/ioutil"
	"json"
	"log"
	"os"
	"strconv"
	"time"
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

func (tw *twitterClient) twitterGet(url string, param web.ParamMap) (p []byte, err os.Error) {
	oauthClient.SignParam(tw.twitterToken, "GET", url, param)
	url = url + "?" + param.FormEncodedString()
	// TODO: Add timeout. 
	var resp *http.Response
	done := make(chan bool, 1)
	go func() {
		resp, _, err = http.Get(url)
		done <- true
	}()

	timeout := time.After(TWITTER_GET_TIMEOUT * 1e9) //
	select {
	case <-done:
		break
	case <-timeout:
		return nil, os.NewError("http Get timed out - " + url)
	}
	if resp == nil {
		panic("oops")
	}
	return readHttpResponse(resp, err)
}

// Data in param must be URL escaped already.
func (tw *twitterClient) twitterPost(url string, param web.ParamMap) (p []byte, err os.Error) {
	oauthClient.SignParam(tw.twitterToken, "POST", url, param)
	//log.Println(param.StringMap())
	return readHttpResponse(http.PostForm(url, param.StringMap()))
}

//func (c *FollowersCrawler) getUserId(screen_name string) (uid int64, err os.Error) {
//	param := make(web.ParamMap)
//	param.Set("screen_name", screen_name)
//	url := TWITTER_API_BASE + "/users/show.json"
//
//	userDetails := map[string]interface{}{}
//	var resp []byte
//
//	if resp, err = c.twitterGet(url, param); err != nil {
//		log.Println("getUserId error", err.String())
//		return
//	}
//	if err = json.Unmarshal(resp, &userDetails); err != nil {
//		log.Println("getUserId unmarshal error", err.String())
//		return
//	}
//	return int64(userDetails["id"].(float64)), nil
//}

func (tw *twitterClient) getUserName(uid int64) (screenName string, err os.Error) {
	param := make(web.ParamMap)
	param.Set("id", strconv.Itoa64(uid))
	url := TWITTER_API_BASE + "/users/show.json"

	userDetails := map[string]interface{}{}
	var resp []byte

	if resp, err = tw.twitterGet(url, param); err != nil {
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

// if uid != 0, search by uid, else by screenName.
func (tw *twitterClient) getUserFollowers(uid int64, screenName string) (uf *userFollowers, err os.Error) {
	param := make(web.ParamMap)
	if uid != 0 {
		param.Set("id", strconv.Itoa64(uid))
	} else {
		param.Set("screen_name", screenName)
	}
	url := TWITTER_API_BASE + "/followers/ids.json"

	var resp []byte
	if resp, err = tw.twitterGet(url, param); err != nil {
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

// Should be "sendDirectMessage".
func (tw *twitterClient) NotifyUnfollower(abandonedName, unfollowerName string) (err os.Error) {
	url := TWITTER_API_BASE + "/direct_messages/new.json"
	log.Printf("%s unfollowed %s, notifying.\n", unfollowerName, abandonedName)
	param := make(web.ParamMap)
	param.Set("screen_name", abandonedName)
	// TODO(nictuku): translate messages.
	param.Set("text", fmt.Sprintf("Xiiii.. você não está mais sendo seguido por @%s :-(.", unfollowerName))
	var p []byte
	if p, err = tw.twitterPost(url, param); err != nil {
		log.Println("notify unfollower error:", err.String())
		log.Println("response", string(p))
	} else {
		log.Printf("Notified %v of unfollow by %v", abandonedName, unfollowerName)
	}
	return
}

func (tw *twitterClient) FollowUser(uid int64) (err os.Error) {
	url := TWITTER_API_BASE + "/friendships/create.json"
	param := make(web.ParamMap)
	param.Set("user_id", strconv.Itoa64(uid))
	param.Set("follow", "true")
	var p []byte
	log.Println("Trying to follow user", uid)
	if p, err = tw.twitterPost(url, param); err != nil {
		log.Println("follower user error:", err.String())
		fmt.Println("response", string(p))
	}
	return
}

func readHttpResponse(resp *http.Response, httpErr os.Error) (p []byte, err os.Error) {
	err = httpErr
	if err != nil {
		log.Println(err.String())
		return nil, err
	}
	p, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	rateLimitStats(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		log.Printf("Response: %s\n", string(p))
		log.Printf("Response Header: %+v", resp.Header)
		err = os.NewError(fmt.Sprintf("Server Error code: %d", resp.StatusCode))
		if err == nil {
			err = os.NewError("HTTP Error " + string(resp.StatusCode) + " (error state _not_ reported by http library)")
		}
		// Better ignore whatever response was given.
		return nil, err
	}
	return p, nil

}

func rateLimitStats(resp *http.Response) {
	if resp == nil {
		return
	}
	curr := time.Seconds()
	reset, _ := strconv.Atoi64(resp.GetHeader("X-RateLimit-Reset"))
	remaining, _ := strconv.Atoi64(resp.GetHeader("X-RateLimit-Remaining"))
	if remaining < 1 && reset-curr > 0 {
		log.Printf("Twitter API limits exceeded. Sleeping for %d seconds.\n", reset-curr)
		time.Sleep((reset - curr) * 1e9)
	}
}
