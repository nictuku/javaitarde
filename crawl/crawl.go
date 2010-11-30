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
	"github.com/edsrzf/mongogo"
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
	// Can't use mongogo for Inserts because of this:
	// https://github.com/edsrzf/mongogo/issues/issue/1
	gomongo "github.com/mikejs/gomongo/mongo"
)

const (
	TWITTER_API_BASE              = "http://api.twitter.com/1"
	UNFOLLOW_DB                   = "unfollow3"
	USER_FOLLOWERS_TABLE          = "user_followers"
	USER_FOLLOWERS_COUNTERS_TABLE = "user_followers_counters"
	FOLLOW_PENDING                = "follow_pending"
)

var dryRunMode bool
var ignoredUsers string

var oauthClient = oauth.Client{
	Credentials:                   oauth.Credentials{clientToken, clientSecret},
	TemporaryCredentialRequestURI: "http://api.twitter.com/oauth/request_token",
	ResourceOwnerAuthorizationURI: "http://api.twitter.com/oauth/authenticate",
	TokenRequestURI:               "http://api.twitter.com/oauth/access_token",
}

func NewFollowersCrawler() *FollowersCrawler {
	// Connect with both mongo libraries. We use them at different times, avoiding bugs :-(.
	conn, err := mongo.Dial("127.0.0.1:27017")
	if err != nil {
		log.Println("mongo Connect error:", err.String())
		panic("mongo conn err")
	}
	conn2, err := gomongo.Connect("127.0.0.1")
	if err != nil {
		log.Println("gomongo Connect error:", err.String())
		panic("mongo conn err")
	}
	return &FollowersCrawler{
		twitterToken: &oauth.Credentials{accessToken, accessTokenSecret},
		mongoConn:    conn,
		gomongoConn:  conn2,
		ourUsers:     make([]int64, 0),
	}
}

type FollowersCrawler struct {
	twitterToken *oauth.Credentials
	mongoConn    *mongo.Conn
	gomongoConn  *gomongo.Connection
	ourUsers     []int64
}

// This is broken. (gomongo date marshaling)
//func (c *FollowersCrawler) InsertThisIsBroken(uf bson.Doc) (err os.Error) {
//	coll := c.mongoConn.Database(UNFOLLOW_DB).Collection(USER_FOLLOWERS_TABLE)
//	// Bug with gomongo, old date.
//	// log.Println("date===>", uf["date"])
//	coll.Insert(uf)
//
//	// Update counters table.
//	counter := bson.Doc{
//		"uid":            uf["uid"],
//		"date":           uf["date"],
//		"followerscount": len(uf["followers"].([]int64)),
//	}
//	coll = c.mongoConn.Database(UNFOLLOW_DB).Collection(USER_FOLLOWERS_COUNTERS_TABLE)
//	return coll.Insert(counter)
//}

// Insert updates two collecitons: the user followers table, and the user followers table counters. 
// The first will be garbage collected later to remove older items. The counters table will be kept forever.
func (c *FollowersCrawler) Insert(uf bson.Doc) (err os.Error) {
	if dryRunMode {
		return
	}
	coll := c.gomongoConn.GetDB(UNFOLLOW_DB).GetCollection(USER_FOLLOWERS_TABLE)
	m, _ := gomongo.Marshal(uf)
	coll.Insert(m)

	// Update counters table.
	counter := bson.Doc{
		"uid":            uf["uid"],
		"date":           uf["date"],
		"followerscount": len(uf["followers"].([]int64)),
	}
	coll = c.gomongoConn.GetDB(UNFOLLOW_DB).GetCollection(USER_FOLLOWERS_COUNTERS_TABLE)
	m, _ = gomongo.Marshal(counter)
	return coll.Insert(m)
}

func (c *FollowersCrawler) MarkPendingFollow(uid int64) os.Error {
	doc := bson.Doc{
		"uid":  uid,
		"date": time.UTC(),
	}
	coll := c.gomongoConn.GetDB(UNFOLLOW_DB).GetCollection(FOLLOW_PENDING)
	m, _ := gomongo.Marshal(doc)
	return coll.Insert(m)
}

func (c *FollowersCrawler) twitterGet(url string, param web.StringsMap) (p []byte, err os.Error) {
	oauthClient.SignParam(c.twitterToken, "GET", url, param)
	url = url + "?" + string(param.FormEncode())
	resp, _, err := http.Get(url)
	return readHttpResponse(resp, err)
}

// Data in param must be URL escaped already.
func (c *FollowersCrawler) twitterPost(url string, param web.StringsMap) (p []byte, err os.Error) {
	oauthClient.SignParam(c.twitterToken, "POST", url, param)
	log.Println(param.StringMap())
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
	if err = c.Insert(uf); err != nil {
		log.Println("Insert error", err.String())
	}
	log.Printf("updated: %d\n", uid)
	return
}

func (c *FollowersCrawler) dbReconnect() {
	log.Printf("reconnecting, just in case")
	conn, err := mongo.Dial("127.0.0.1:27017")
	if err != nil {
		log.Println("mongo Connect error:", err.String())
		panic("mongo conn err")
	}
	conn2, err := gomongo.Connect("127.0.0.1")
	if err != nil {
		log.Println("gomongo Connect error:", err.String())
		panic("mongo conn err")
	}
	c.mongoConn = conn
	c.gomongoConn = conn2
}

func (c *FollowersCrawler) dbGetIsFollowPending(uid int64) (isPending bool, err os.Error) {
	db := c.mongoConn.Database(UNFOLLOW_DB)
	col := db.Collection(FOLLOW_PENDING)
	query := mongo.Query{"uid": uid}
	cursor, err := col.Query(query, 0, 1)
	if err != nil {
		log.Printf("dbGetFollowPending: uid=%d, cursor error %s", uid, err.String())
		c.dbReconnect()
		return false, err
	}
	defer cursor.Close()
	f := cursor.Next()
	return f != nil, nil
}

func (c *FollowersCrawler) dbGetUserFollowers(uid int64) (uf bson.Doc, err os.Error) {
	db := c.mongoConn.Database(UNFOLLOW_DB)
	col := db.Collection(USER_FOLLOWERS_TABLE)
	query := mongo.Query{"uid": uid}
	sort := map[string]int32{"date": -1}
	query.Sort(sort)
	cursor, err := col.Query(query, 0, 1)
	if err != nil {
		log.Printf("uid=%d, cursor error %s", uid, err.String())
		c.dbReconnect()
		return
	}
	defer cursor.Close()
	uf = cursor.Next()
	if uf == nil {
		err = os.NewError("no items found")
		return
	}
	if _, ok := uf["followers"]; !ok {
		log.Println("followers not set?")
	}
	return
}

func (c *FollowersCrawler) DiffFollowers(abandonedUser int64, prevUf, newUf bson.Doc) {
	fOld, ok := prevUf["followers"]
	if !ok || fOld == nil {
		log.Printf("fOld: no followers %+v", fOld)
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
			c.dbReconnect()
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
	if dryRunMode {
		return
	}
	url := TWITTER_API_BASE + "/direct_messages/new.json"
	abandoned, err := c.getUserName(abandonedUser)
	param := make(web.StringsMap)
	param.Set("screen_name", abandoned)
	// TODO(nictuku): translate messages.
	param.Set("text", fmt.Sprintf("Xiiii.. você não está mais sendo seguido por @%s :-(.", unfollowerScreenName))
	var p []byte
	if p, err = c.twitterPost(url, param); err != nil {
		log.Println("notify unfollower error:", err.String())
		log.Println("response", string(p))
	} else {
		log.Println("notified.")
	}
	return
}

func (c *FollowersCrawler) FollowUser(uid int64) (err os.Error) {
	if dryRunMode {
		return
	}
	if isPending, _ := c.dbGetIsFollowPending(uid); isPending {
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
	c.MarkPendingFollow(uid)
	return
}

func (c *FollowersCrawler) GetAllUsersFollowers() (err os.Error) {
	for _, u := range c.ourUsers {
		prevUf := bson.Doc{}
		newUf := bson.Doc{}
		if prevUf, err = c.dbGetUserFollowers(u); err != nil {
			log.Println("user", u)
			log.Println("dbGetUserFollowers err", err.String())
			prevUf = nil
		}
		if newUf, err = c.getUserFollowers(u, ""); err != nil {
			log.Println("user", u)
			log.Println("GetUserFollowers err", err.String())
			if strings.Contains(err.String(), " 401") {
				// User's follower list is blocked. Need to request access.
				c.FollowUser(u)
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
	u, _ := c.dbGetUserFollowers(16196534)
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
		log.Printf("HTTP Error %d", resp.StatusCode)
		log.Printf("Response: %s", string(p))
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
	reset, _ := strconv.Atoi64(resp.GetHeader("X-RateLimit-Reset"))
	curr := time.Seconds()
	log.Print("(TwitterRateLimit) Limit:", resp.GetHeader("X-RateLimit-Limit"),
		", Remaining: ", resp.GetHeader("X-RateLimit-Remaining"),
		", Reset in ", reset-curr, "s")
	// TODO(nictuku): Add proper rate limiting based on values above.
	// TODO(nictuku): Maybe panic if we do more than X operations per run, as an extra precaution.
	time.Sleep(3e9) // 3 seconds
}


func init() {
	flag.BoolVar(&dryRunMode, "dryrun", true,
		"Don't make changes to the database.")
	// TODO(nictuku): Make this a list.
	flag.StringVar(&ignoredUsers, "ignoreUsers", "118058049",
		"UserID to ignore (flaky twitter results)")
}
