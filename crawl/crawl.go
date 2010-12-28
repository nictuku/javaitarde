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
	"flag"
	"github.com/edsrzf/go-bson"
	"github.com/garyburd/twister/oauth"
	"github.com/garyburd/twister/web"
	"http"
	"io/ioutil"
	"json"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	TWITTER_API_BASE = "http://api.twitter.com/1"
)

var dryRunMode bool
var notifyUsers bool
var ignoredUsers string
var oauthClient = oauth.Client{
	Credentials:                   oauth.Credentials{clientToken, clientSecret},
	TemporaryCredentialRequestURI: "http://api.twitter.com/oauth/request_token",
	ResourceOwnerAuthorizationURI: "http://api.twitter.com/oauth/authenticate",
	TokenRequestURI:               "http://api.twitter.com/oauth/access_token",
}

type FollowersCrawler struct {
	twitterToken *oauth.Credentials
	ourUsers     []int64
	db           *FollowersDatabase
}

func NewFollowersCrawler() *FollowersCrawler {
	newDb := NewFollowersDatabase()
	return &FollowersCrawler{
		twitterToken: &oauth.Credentials{accessToken, accessTokenSecret},
		db:           newDb,
		ourUsers:     make([]int64, 0),
	}
}

func (c *FollowersCrawler) twitterGet(url string, param web.StringsMap) (p []byte, err os.Error) {
	oauthClient.SignParam(c.twitterToken, "GET", url, param)
	url = url + "?" + param.FormEncodedString()
	resp, _, err := http.Get(url)
	return readHttpResponse(resp, err)
}

// Data in param must be URL escaped already.
func (c *FollowersCrawler) twitterPost(url string, param web.StringsMap) (p []byte, err os.Error) {
	oauthClient.SignParam(c.twitterToken, "POST", url, param)
	//log.Println(param.StringMap())
	return readHttpResponse(http.PostForm(url, param.StringMap()))
}

func (c *FollowersCrawler) getUserId(screen_name string) (uid int64, err os.Error) {
	param := make(web.StringsMap)
	param.Set("screen_name", screen_name)
	url := TWITTER_API_BASE + "/users/show.json"

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

func (c *FollowersCrawler) getUserName(uid int64) (screenName string, err os.Error) {
	param := make(web.StringsMap)
	param.Set("id", strconv.Itoa64(uid))
	url := TWITTER_API_BASE + "/users/show.json"

	userDetails := map[string]interface{}{}
	var resp []byte

	if resp, err = c.twitterGet(url, param); err != nil {
		log.Println("getUserName error", err.String())
		return
	}
	if err = json.Unmarshal(resp, &userDetails); err != nil {
		log.Println("getUserName unmarshal error", err.String())
		return
	}
	return userDetails["screen_name"].(string), nil
}


// if uid != 0, search by uid, else by screenName.
func (c *FollowersCrawler) getUserFollowers(uid int64, screenName string) (uf bson.Doc, err os.Error) {
	param := make(web.StringsMap)
	if uid != 0 {
		param.Set("id", strconv.Itoa64(uid))
	} else {
		param.Set("screen_name", screenName)
	}
	url := TWITTER_API_BASE + "/followers/ids.json"

	var resp []byte
	if resp, err = c.twitterGet(url, param); err != nil {
		return
	}
	var followers []int64
	if err = json.Unmarshal(resp, &followers); err != nil {
		log.Println("unmarshal error", err.String())
		log.Println("output was:", resp)
		return
	}

	uf = bson.Doc{"uid": uid, "followers": followers, "date": time.UTC()}
	if err = c.db.Insert(uf); err != nil {
		log.Println("Insert error", err.String())
	}
	return
}

func (c *FollowersCrawler) DiffFollowers(abandonedUser int64, prevUf, newUf bson.Doc) {
	fOld, ok := prevUf["followers"]
	if !ok || fOld == nil {
		log.Printf("fOld: no followers %+v\n", fOld)
		return
	}
	fNew := newUf["followers"]
	if fNew == nil {
		log.Println("fNew: no followers")
		return
	}
	neww := map[int64]int{}
	for _, uid := range fNew.([]int64) {
		neww[uid] = 1
	}

	if len(fOld.([]interface{})) > len(neww)+5 {
		panic("too many unfollows")
	}

	// We don't care about new followers, only missing ones.
	for _, uid := range fOld.([]interface{}) {
		unfollower := uid.(int64)
		if unfollower < 184 {
			log.Println("ERROR while comparing user ", strconv.Itoa64(abandonedUser))
			log.Println("ERROR: bogus uid found in old database: ", unfollower)
			//panic("bogus uid" + strconv.Itoa64(uid.(int64)))
			c.db.Reconnect()
			continue
		}
		if _, ok := neww[unfollower]; !ok {
			log.Println("SOMEONE STOPPED FOLLOWING OUR USER", strconv.Itoa64(abandonedUser))
			if ignore, _ := strconv.Atoi64(ignoredUsers); ignore == unfollower {
				log.Println("(ignored)")
				continue
			}
			if unfollower == 118058049 {
				log.Println("ignored@@@@@@@@@@@@@@@")
				continue
			}
			if screenName, err := c.getUserName(unfollower); err != nil {
				log.Println(".. but we couldn't get the screenName:", err.String())
			} else {
				log.Println("====>> THIS SUCKER STOP FOLLOWING THEM:", screenName, unfollower)
				// TODO(nictuku): mark database entry as processed if there were no errors.
				c.NotifyUnfollower(abandonedUser, screenName)
			}
		}
	}
}

func (c *FollowersCrawler) NotifyUnfollower(abandonedUser int64, unfollowerScreenName string) (err os.Error) {
	if dryRunMode || !notifyUsers {
		return
	}
	url := TWITTER_API_BASE + "/direct_messages/new.json"
	abandoned, err := c.getUserName(abandonedUser)
	log.Printf("%s unfollowed %s, notifying.\n", unfollowerScreenName, abandoned)
	param := make(web.StringsMap)
	param.Set("screen_name", abandoned)
	// TODO(nictuku): translate messages.
	param.Set("text", fmt.Sprintf("Xiiii.. você não está mais sendo seguido por @%s :-(.", unfollowerScreenName))
	var p []byte
	if p, err = c.twitterPost(url, param); err != nil {
		log.Println("notify unfollower error:", err.String())
		log.Println("response", string(p))
	}
	return
}

func (c *FollowersCrawler) FollowUser(uid int64) (err os.Error) {
	if dryRunMode {
		return
	}
	if isPending, _ := c.db.GetIsFollowingPending(uid); isPending {
		log.Println("Already trying to follow user. Skipping follow request.")
		return
	}
	url := TWITTER_API_BASE + "/friendships/create.json"
	param := make(web.StringsMap)
	param.Set("user_id", strconv.Itoa64(uid))
	param.Set("follow", "true")
	var p []byte
	log.Println("Trying to follow user", uid)
	if p, err = c.twitterPost(url, param); err != nil {
		log.Println("follower user error:", err.String())
		fmt.Println("response", string(p))
		return err
	}
	c.db.MarkPendingFollow(uid)
	return
}

func (c *FollowersCrawler) GetAllUsersFollowers() (err os.Error) {
	for _, u := range c.ourUsers {
		prevUf := bson.Doc{}
		newUf := bson.Doc{}
		if prevUf, err = c.db.GetUserFollowers(u); err != nil {
			log.Printf("db.GetUserFollowers err=%s, userId=%d\n", err.String(), u)
			prevUf = nil
		}
                // TODO(nictuku): Currently, interrupted executions will update the database even though the users were
                // not notified. Need to either mark entries as 'processed' or only save them on the database post fact.
		if newUf, err = c.getUserFollowers(u, ""); err != nil {
			if strings.Contains(err.String(), " 401") {
				// User's follower list is blocked. Need to request access.
				c.FollowUser(u)
			} else {
				log.Printf("TwitterGetUserFollowers err=%s, userId=%d\n", err.String(), u)
			}
			newUf = nil
		}
		if prevUf != nil && newUf != nil {
			c.DiffFollowers(u, prevUf, newUf)
		}
	}
	return
}

// Find everyone who follows us, so we know who to crawl.
func (c *FollowersCrawler) FindOurUsers(uid int64) (err os.Error) {
	//c.ourUsers = []int64{217554981}
	//return nil
	userFollowers, err := c.getUserFollowers(uid, "")
	if err != nil {
		return err
	}
	c.ourUsers = userFollowers["followers"].([]int64)
	return
}

func (c *FollowersCrawler) TestStuff() {
	log.Println("gogo")
	u, _ := c.db.GetUserFollowers(16196534)
	for f1, f2 := range u["followers"].([]interface{}) {
		if f2.(int64) == 118058049 {
			log.Println(f1, f2)
		}
		if f2.(int64) < 1000 {
			log.Println(f1, f2)
		}
	}
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
		time.Sleep(reset-curr * 1e9)
	}
}


func init() {
	flag.BoolVar(&dryRunMode, "dryrun", true,
		"Don't make changes to the database.")
	flag.BoolVar(&notifyUsers, "notifyUsers", true,
		"Notify unfollows to users.")
	// TODO(nictuku): Make this a list.
	flag.StringVar(&ignoredUsers, "ignoreUsers", "118058049",
		"UserID to ignore (flaky twitter results)")
}
