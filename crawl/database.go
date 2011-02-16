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
	"github.com/edsrzf/mongogo"
	// Can't use mongogo for Inserts because of this:
	// https://github.com/edsrzf/mongogo/issues/issue/1
	gomongo "github.com/mikejs/gomongo/mongo"
)

const (
	UNFOLLOW_DB                   = "unfollow3"
	USER_FOLLOWERS_TABLE          = "user_followers"
	USER_FOLLOWERS_COUNTERS_TABLE = "user_followers_counters"
	FOLLOW_PENDING                = "follow_pending"
)

type FollowersDatabase struct {
	mongoConn   *mongo.Conn
	gomongoConn *gomongo.Connection
}

func NewFollowersDatabase() *FollowersDatabase {
	// Connect with both mongo libraries. We use them at different times,
	// to avoid bugs :-(.
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
	return &FollowersDatabase{mongoConn: conn, gomongoConn: conn2}
}

// Insert updates two collecitons: the user followers table, and the user followers table counters. 
// The first will be garbage collected later to remove older items. The counters table will be kept forever.
func (c *FollowersDatabase) Insert(uf bson.Doc) (err os.Error) {
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


func (c *FollowersDatabase) MarkPendingFollow(uid int64) os.Error {
	doc := bson.Doc{
		"uid":  uid,
		"date": time.UTC(),
	}
	coll := c.gomongoConn.GetDB(UNFOLLOW_DB).GetCollection(FOLLOW_PENDING)
	m, _ := gomongo.Marshal(doc)
	return coll.Insert(m)
}

func (c *FollowersDatabase) Reconnect() {
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

func (c *FollowersDatabase) GetIsFollowingPending(uid int64) (isPending bool, err os.Error) {
	db := c.mongoConn.Database(UNFOLLOW_DB)
	col := db.Collection(FOLLOW_PENDING)
	query := mongo.Query{"uid": uid}
	cursor, err := col.Find(query, 0, 1)
	if err != nil {
		log.Printf("dbGetFollowPending: uid=%d, cursor error %s", uid, err.String())
		c.Reconnect()
		return false, err
	}
	defer cursor.Close()
	f := cursor.Next()
	return f != nil, nil
}

func (c *FollowersDatabase) GetUserFollowers(uid int64) (uf bson.Doc, err os.Error) {
	db := c.mongoConn.Database(UNFOLLOW_DB)
	col := db.Collection(USER_FOLLOWERS_TABLE)
	query := mongo.Query{"uid": uid}
	sort := map[string]int32{"date": -1}
	query.Sort(sort)
	cursor, err := col.Find(query, 0, 1)
	if err != nil {
		log.Printf("uid=%d, cursor error %s", uid, err.String())
		c.Reconnect()
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
