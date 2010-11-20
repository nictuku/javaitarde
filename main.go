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

package main

import (
	"flag"
	"github.com/garyburd/twister/oauth"
	"github.com/garyburd/twister/web"
	"github.com/mikejs/gomongo/mongo"
	"http"
	"io/ioutil"
	"json"
	"log"
	"os"
)

type userFollowers struct {
	uid       int64
	followers []int64
}

const (
	TWITTER_API_BASE = "http://api.twitter.com/1"
	UNFOLLOW_DB = "unfollow"
	USER_FOLLOWERS_TABLE = "user_followers"
)

var oauthClient = oauth.Client{
	Credentials:                   oauth.Credentials{clientToken, clientSecret},
	TemporaryCredentialRequestURI: "http://api.twitter.com/oauth/request_token",
	ResourceOwnerAuthorizationURI: "http://api.twitter.com/oauth/authenticate",
	TokenRequestURI:               "http://api.twitter.com/oauth/access_token",
}

func NewFollowersCrawler() *FollowersCrawler {
	conn, err := mongo.Connect("127.0.0.1")
	if err != nil {
		log.Println("mongo Connect error:", err.String())
	}
	return &FollowersCrawler{
		twitterToken: &oauth.Credentials{accessToken, accessTokenSecret},
		mongoConn:    conn,
	}
}

type FollowersCrawler struct {
	twitterToken *oauth.Credentials
	mongoConn    *mongo.Connection
}

func (c *FollowersCrawler) Save(document mongo.BSON) os.Error {
	coll := c.mongoConn.GetDB(UNFOLLOW_DB).GetCollection(USER_FOLLOWERS_TABLE)
	coll.Insert(document)
	log.Println("Inserted Document")
	return nil
}

func (c *FollowersCrawler) twitterGet(url string, param web.StringsMap) (p []byte, err os.Error) {
	oauthClient.SignParam(c.twitterToken, "GET", url, param)
	url = url + "?" + string(param.FormEncode())
	log.Println(url)
	resp, _, err := http.Get(url)
	if err != nil {
		log.Println(err.String())
		return nil, err
	}
	p, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, err
	}
	return p, nil
}

func (c *FollowersCrawler) getUserId(screen_name string) (uid int64, err os.Error) {
	param := make(web.StringsMap)
	param.Set("screen_name", screen_name)
	url := TWITTER_API_BASE + "/users/show.json"

	// Will ignore all string fields.
	userDetails := map[string]interface{}{}
	var resp []byte

	if resp, err = c.twitterGet(url, param); err != nil {
		log.Println("getUserId error", err.String())
		return
	}
	if err = json.Unmarshal(resp, &userDetails); err != nil {
		log.Println("getUserId unmarshal error", err.String())
		return
	}
	return int64(userDetails["id"].(float64)), nil
}

func (c *FollowersCrawler) getUserFollowers(screen_name string) (err os.Error) {
	var uid int64
	if uid, err = c.getUserId(screen_name); err != nil {
		return
	}

	param := make(web.StringsMap)
	param.Set("screen_name", screen_name)
	url := TWITTER_API_BASE + "/followers/ids.json"

	var resp []byte
	if resp, err = c.twitterGet(url, param); err != nil {
		return
	}
	var followers []int64
	if err = json.Unmarshal(resp, &followers); err != nil {
		log.Println("unmarshal error", err.String())
		return
	}

	g := userFollowers{uid: uid, followers: followers}
	if document, err := mongo.Marshal(g); err != nil {
		log.Println("err", err.String())
		return
	} else {
		c.Save(document)
	}
	return
}

func main() {
	flag.Parse()

	crawler := NewFollowersCrawler()
	crawler.getUserFollowers("javaitarde")
}
