// Copyright 2010 Gary Burd
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
	"bytes"
	"flag"
	"fmt"
	"github.com/garyburd/twister/oauth"
//	"github.com/garyburd/twister/server"
	"github.com/garyburd/twister/web"
	"http"
	"io/ioutil"
	"json"
	"log"
//	"os"
//	"strings"
//	"template"
)

var oauthClient = oauth.Client{
	// This sets various things, including:
	// oauth_consumer_key
	// oauth_token
	// and: _timestamp, _version, etc.
	Credentials:                   oauth.Credentials{clientToken, clientSecret},
	TemporaryCredentialRequestURI: "http://api.twitter.com/oauth/request_token",
	ResourceOwnerAuthorizationURI: "http://api.twitter.com/oauth/authenticate",
	TokenRequestURI:               "http://api.twitter.com/oauth/access_token",
}
// home handles requests to the home page.
func getUserFollowers() {
	token := &oauth.Credentials{accessToken, accessTokenSecret}

	param := make(web.StringsMap)
	url := "http://api.twitter.com/1/statuses/home_timeline.json"
	oauthClient.SignParam(token, "GET", url, param)
	url = url + "?" + string(param.FormEncode())
	log.Println(url)
	resp, _, err := http.Get(url)
	if err != nil {
		log.Println(err.String())
		return
	}
	p, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Println(err.String())
		return
	}
	if resp.StatusCode != 200 {
		log.Println("error from twitter:", resp.StatusCode)
		return
	}
	//w := req.Respond(web.StatusOK, web.HeaderContentType, "text/plain")
	var buf bytes.Buffer
	json.Indent(&buf, p, "", "  ")
	log.Println("START")
	fmt.Println(buf.String())
	log.Println("END")
}

func main() {
	flag.Parse()
	getUserFollowers()
//		Register("/login", "GET", login).
//		Register("/account/twitter-callback", "GET", twitterCallback).
//		Register("/twitter-callback", "GET", twitterCallback)))

}
