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
	"flag"
	"github.com/garyburd/go-mongo/mongo"
	"log"
	"os"
	"time"
)

var (
	DbName       string
	verboseMongo bool
)

const (
	USER_FOLLOWERS_TABLE          = "user_followers"
	USER_FOLLOWERS_COUNTERS_TABLE = "user_followers_counters"
	FOLLOW_PENDING_TABLE          = "follow_pending"
	PREVIOUS_UNFOLLOWS_TABLE      = "previous_unfollows"
)

func init() {
	flag.StringVar(&DbName, "database", "unfollow",
		"Name of mongo database.")
	flag.BoolVar(&verboseMongo, "verboseMongo", false,
		"Log all mongo queries.")
}

type FollowersDatabase struct {
	userFollowers        mongo.Collection
	userFollowersCounter mongo.Collection
	followPending        mongo.Collection
	previousUnfollows    mongo.Collection
}

func NewFollowersDatabase() *FollowersDatabase {
	conn, err := mongo.Dial("127.0.0.1:27017")
	if err != nil {
		log.Println("mongo Connect error:", err.Error())
		panic("mongo conn err")
	}
	if verboseMongo {
		conn = mongo.NewLoggingConn(conn, log.New(os.Stderr, "", 0), "")
	}
	db := mongo.Database{conn, DbName, mongo.DefaultLastErrorCmd}
	return &FollowersDatabase{
		userFollowers:        db.C(USER_FOLLOWERS_TABLE),
		userFollowersCounter: db.C(USER_FOLLOWERS_COUNTERS_TABLE),
		followPending:        db.C(FOLLOW_PENDING_TABLE),
		previousUnfollows:    db.C(PREVIOUS_UNFOLLOWS_TABLE),
	}
}

// Insert updates two collections: the user followers table, and the user followers table counters. 
// The first will be garbage collected later to remove older items. The counters table will be kept forever.
func (c *FollowersDatabase) Insert(uf *userFollowers) (err error) {
	if dryRunMode {
		return
	}
	err = c.userFollowers.Insert(uf)
	if err != nil {
		return err
	}

	// Update counters table.
	counter := map[string]interface{}{
		"uid":            uf.Uid,
		"date":           uf.Date,
		"followerscount": len(uf.Followers),
	}
	return c.userFollowersCounter.Insert(counter)
}

func (c *FollowersDatabase) MarkPendingFollow(uid int64) error {
	doc := map[string]interface{}{
		"uid":  uid,
		"date": time.Now().UTC().Unix(),
	}
	return c.followPending.Insert(doc)
}

func (c *FollowersDatabase) Reconnect() {
	log.Printf("reconnecting, just in case")
	conn, err := mongo.Dial("127.0.0.1:27017")
	if err != nil {
		log.Println("mongo Connect error:", err.Error())
		panic("mongo conn err")
	}
	c.userFollowers.Conn = conn
	c.userFollowersCounter.Conn = conn
	c.followPending.Conn = conn
	c.previousUnfollows.Conn = conn
}

func (c *FollowersDatabase) GetIsFollowingPending(uid int64) (isPending bool, err error) {
	cursor, err := c.followPending.Find(map[string]int64{"uid": uid}).Cursor()
	if err != nil {
		return false, nil
	}
	defer cursor.Close()
	return cursor.HasNext(), nil
}

func (c *FollowersDatabase) GetWasUnfollowNotified(abandonedUser, unfollower int64) (wasNotified bool) {
	query := map[string]int64{
		"uid":        abandonedUser,
		"unfollower": unfollower,
	}
	cursor, err := c.previousUnfollows.Find(query).Cursor()
	if err != nil {
		return false
	}
	defer cursor.Close()
	return cursor.HasNext()
}

func (c *FollowersDatabase) MarkUnfollowNotified(abandonedUser, unfollower int64) error {
	doc := map[string]int64{
		"uid":        abandonedUser,
		"unfollower": unfollower,
	}
	return c.previousUnfollows.Insert(doc)
}

func (c *FollowersDatabase) GetUserFollowers(uid int64) (uf *userFollowers, err error) {
	cursor, err := c.userFollowers.Find(&mongo.QuerySpec{
		Query: mongo.M{"uid": uid},
		Sort:  mongo.D{{"date", -1}},
	}).Cursor()
	if err != nil {
		return
	}
	defer cursor.Close()

	if !cursor.HasNext() {
		return
	}
	err = cursor.Next(&uf)

	if uf == nil {
		log.Println("uf object remained nil. Bug in go-mongo?")
	} else if uf.Followers == nil {
		log.Println("uf.Followers is nil. Incorrect database schema or bson decoding?")
	}
	return
}
