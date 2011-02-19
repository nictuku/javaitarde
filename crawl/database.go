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
	"log"
	"os"
	"time"
	"github.com/edsrzf/go-bson"
	"github.com/garyburd/go-mongo"
)

const (
	db                            = "unfollow3"
	USER_FOLLOWERS_TABLE          = db + ".user_followers"
	USER_FOLLOWERS_COUNTERS_TABLE = db + ".user_followers_counters"
	FOLLOW_PENDING_TABLE          = db + ".follow_pending"
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
	return &FollowersDatabase{mongoConn: conn}
}

// Insert updates two collecitons: the user followers table, and the user followers table counters. 
// The first will be garbage collected later to remove older items. The counters table will be kept forever.
func (c *FollowersDatabase) Insert(uf bson.Doc) (err os.Error) {
	if dryRunMode {
		return
	}
	err = mongo.SafeInsert(c.mongoConn, USER_FOLLOWERS_TABLE, nil, uf)
	if err != nil {
		return err
	}

	// Update counters table.
	counter := bson.Doc{
		"uid":            uf["uid"],
		"date":           uf["date"],
		"followerscount": len(uf["followers"].([]int64)),
	}
	return mongo.SafeInsert(c.mongoConn, USER_FOLLOWERS_COUNTERS_TABLE, nil, counter)
}


func (c *FollowersDatabase) MarkPendingFollow(uid int64) os.Error {
	doc := bson.Doc{
		"uid":  uid,
		"date": time.UTC(),
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

func (c *FollowersDatabase) GetUserFollowers(uid int64) (uf map[string]interface{}, err os.Error) {
	order := map[string]int64{"date": 1}
	q := map[string]interface{}{"uid": uid}
	query := map[string]interface{}{"$query": q, "$orderby": order}

	cursor, err := c.mongoConn.Find(USER_FOLLOWERS_TABLE, query, nil)
	if !cursor.HasNext() {
		err = os.NewError("no result")
		return
	}
	err = cursor.Next(&uf)
	if _, ok := uf["followers"]; !ok {
		log.Println("followers not set?")
	}
	return
}
