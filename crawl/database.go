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
	"log"
	"os"
	"time"
	"github.com/garyburd/go-mongo"
)

var (
	db                            string
	USER_FOLLOWERS_TABLE          string
	USER_FOLLOWERS_COUNTERS_TABLE string
	FOLLOW_PENDING_TABLE          string
	PREVIOUS_UNFOLLOWS_TABLE      string
	verboseMongo                  bool
)

type FollowersDatabase struct {
	mongoConn mongo.Conn
}

func NewFollowersDatabase() *FollowersDatabase {
	conn, err := mongo.Dial("127.0.0.1:27017")
	if err != nil {
		log.Println("mongo Connect error:", err.String())
		panic("mongo conn err")
	}
	if verboseMongo {
		conn = mongo.NewLoggingConn(conn)
	}
	return &FollowersDatabase{mongoConn: conn}
}

// Insert updates two collections: the user followers table, and the user followers table counters. 
// The first will be garbage collected later to remove older items. The counters table will be kept forever.
func (c *FollowersDatabase) Insert(uf *userFollowers) (err os.Error) {
	if dryRunMode {
		return
	}
	err = mongo.SafeInsert(c.mongoConn, USER_FOLLOWERS_TABLE, nil, uf)
	if err != nil {
		return err
	}

	// Update counters table.
	counter := map[string]interface{}{
		"uid":            uf.Uid,
		"date":           uf.Date,
		"followerscount": len(uf.Followers),
	}
	return mongo.SafeInsert(c.mongoConn, USER_FOLLOWERS_COUNTERS_TABLE, nil, counter)
}

func (c *FollowersDatabase) MarkPendingFollow(uid int64) os.Error {
	doc := map[string]interface{}{
		"uid":  uid,
		"date": time.UTC().Seconds(),
	}
	return mongo.SafeInsert(c.mongoConn, FOLLOW_PENDING_TABLE, nil, doc)
}

func (c *FollowersDatabase) Reconnect() {
	log.Printf("reconnecting, just in case")
	conn, err := mongo.Dial("127.0.0.1:27017")
	if err != nil {
		log.Println("mongo Connect error:", err.String())
		panic("mongo conn err")
	}
	c.mongoConn = conn
}

func (c *FollowersDatabase) GetIsFollowingPending(uid int64) (isPending bool, err os.Error) {
	cursor, err := c.mongoConn.Find(FOLLOW_PENDING_TABLE, map[string]int64{"uid": uid}, nil)
	defer cursor.Close()
	if cursor.HasNext() {
		return true, nil
	}
	return false, err
}

func (c *FollowersDatabase) GetWasUnfollowNotified(abandonedUser, unfollower int64) (wasNotified bool) {
	query := map[string]int64{
		"uid":        abandonedUser,
		"unfollower": unfollower,
	}
	cursor, _ := c.mongoConn.Find(PREVIOUS_UNFOLLOWS_TABLE, query, nil)
	defer cursor.Close()
	return cursor.HasNext()
}

func (c *FollowersDatabase) MarkUnfollowNotified(abandonedUser, unfollower int64) os.Error {
	doc := map[string]int64{
		"uid":        abandonedUser,
		"unfollower": unfollower,
	}
	return mongo.SafeInsert(c.mongoConn, PREVIOUS_UNFOLLOWS_TABLE, nil, doc)
}

func (c *FollowersDatabase) GetUserFollowers(uid int64) (uf *userFollowers, err os.Error) {
	collection := mongo.Collection{c.mongoConn, USER_FOLLOWERS_TABLE, mongo.DefaultLastErrorCmd}
	cursor, err := collection.Find(&mongo.QuerySpec{
		Query: mongo.Doc{{"uid", uid}},
		Sort:  mongo.Doc{{"date", -1}},
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

// For testing.
func SetupTestDb(testDb string) {
	db = testDb
	SetupDb()
}

func SetupDb() {
	USER_FOLLOWERS_TABLE = db + ".user_followers"
	USER_FOLLOWERS_COUNTERS_TABLE = db + ".user_followers_counters"
	FOLLOW_PENDING_TABLE = db + ".follow_pending"
	PREVIOUS_UNFOLLOWS_TABLE = db + ".previous_unfollows"

}

func init() {
	flag.StringVar(&db, "database", "unfollow3",
		"Name of mongo database.")
	flag.BoolVar(&verboseMongo, "verboseMongo", false,
		"Log all mongo queries.")
}
