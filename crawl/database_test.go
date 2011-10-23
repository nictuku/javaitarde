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
	"testing"
	"reflect"
)

var testUser = int64(112161284)

const (
	testDb = "unfollowDEV"
)

func init() {
	SetupTestDb(testDb)
}
// This (flaky) test was created to reproduce a bug, that I later confirmed to
// be in mongodb-unstable only.
// https://github.com/edsrzf/mongogo/issues/closed#issue/2
// Also used to debug a problem with bson decoding.
func TestMongo(t *testing.T) {
	c := NewFollowersCrawler()
	u, err := c.db.GetUserFollowers(testUser)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatalf("db.GetUserFollowers(%s) returned nil. Verify if the test database is setup properly or if there's a bug in the javaitarde or go-mongo code", testUser)
	}
	if len(u.Followers) == 0 {
		t.Fatal("No users found in test database. Verify if it's properly setup or if there is a problem with the mongo driver or server")
	}
	for i := 0; i < 100; i++ {
		u2, _ := c.db.GetUserFollowers(testUser)
		if !reflect.DeepEqual(u, u2) {
			t.Errorf("#%d expected\n%v\ngot\n%v", i, u, u2)
		}
	}
}
