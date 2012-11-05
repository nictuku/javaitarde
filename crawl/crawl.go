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
	"errors"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
)

const maxErrors = 5

var (
	dryRunMode   bool
	ignoredUsers string
	maxUnfollows int
	notifyUsers  bool
)

func init() {
	flag.BoolVar(&dryRunMode, "dryrun", true,
		"Don't make changes to the database.")
	flag.BoolVar(&notifyUsers, "notifyUsers", true,
		"Notify unfollows to users.")
	flag.IntVar(&maxUnfollows, "maxUnfollows", 50, "Panic if the number of unfollows for a user exceeds this.")
	// TODO(nictuku): Make this a list.
	flag.StringVar(&ignoredUsers, "ignoreUsers", "118058049",
		"UserID to ignore (flaky twitter results)")
}

type FollowersCrawler struct {
	ourUsers []int64
	userMap  map[int64]string
	db       *FollowersDatabase
	tw       *twitterClient
}

func NewFollowersCrawler() *FollowersCrawler {
	return &FollowersCrawler{
		tw:       newTwitterClient(),
		db:       NewFollowersDatabase(),
		ourUsers: make([]int64, 0),
		userMap:  map[int64]string{},
	}
}

// Find everyone who follows us, so we know who to crawl.
func (c *FollowersCrawler) FindOurUsers(uid int64) (err error) {
	if err := c.tw.verifyCredentials(); err != nil {
		return err
	}
	uf, err := c.tw.getUserFollowers(uid, "")
	if err != nil {
		return err
	}
	if err := c.saveUserFollowers(uf); err != nil {
		log.Printf("c.saveUserFollowers(), u=%v, err=%v", uid, err)
	}
	c.ourUsers = uf.Followers
	return
}

func (c *FollowersCrawler) GetAllUsersFollowers() (err error) {
	var (
		prevUf     *userFollowers
		newUf      *userFollowers
		errorCount = 0
	)
	for _, u := range c.ourUsers {
		if errorCount >= maxErrors {
			return errors.New(fmt.Sprintf("Too many errors (%d). Aborting GetAllUsersFollowers(). ", errorCount))
		}
		if prevUf, err = c.db.GetUserFollowers(u); err != nil {
			log.Printf("GetAllUserFollowers err=%s, userId=%d\n", err.Error(), u)
			// Give up if we can't read from the database.
			// This assumes that a new user will return an empty
			// value, without errors.
			continue
		}
		if newUf, err = c.tw.getUserFollowers(u, ""); err != nil {
			if strings.Contains(err.Error(), " 401") {
				// User's follower list is blocked. Need to request access.
				if err := c.FollowUser(u); err != nil {
					log.Println("FollowUser:", err)
				}
			} else {
				log.Printf("TwitterGetUserFollowers err=%s, userId=%d\n", err.Error(), u)
				errorCount += 1
			}
			continue
		}
		if newUf == nil {
			log.Println("No followers found in twitter for user", u)
			errorCount += 1
			continue
		}
		for _, unfollower := range c.DiffFollowers(u, prevUf, newUf) {
			if err := c.ProcessUnfollow(u, unfollower); err != nil {
				log.Printf("ProcessUnfollow failure, userId=%d, unfollower=%v. Err: %v", u, unfollower, err)
				errorCount += 1
				continue
			}
		}
		// Only save to DB if all went fine.
		if err := c.saveUserFollowers(newUf); err != nil {
			log.Printf("c.saveUserFollowers(), u=%d, err=%v", u, err)
			errorCount += 1
			continue
		}
		errorCount = 0
	}
	return
}

func (c *FollowersCrawler) getUserName(uid int64) (screenName string, err error) {
	// TODO: Save in our database.
	if screenName, ok := c.userMap[uid]; ok {
		return screenName, nil
	}
	if screenName, err = c.tw.getUserName(uid); err == nil {
		c.userMap[uid] = screenName
	}
	return
}

func (c *FollowersCrawler) saveUserFollowers(uf *userFollowers) (err error) {
	if uf == nil {
		return errors.New("saveUserFollowers() called with a nil `userFollowers` object.")
	}
	if dryRunMode {
		log.Println("dryRunMode, skipping saveUserFollowers")
		return
	}
	if err = c.db.Insert(uf); err != nil {
		log.Println("Insert error", err.Error())
	}
	return
}

func (c *FollowersCrawler) DiffFollowers(abandonedUser int64, prevUf, newUf *userFollowers) (unfollowers []int64) {
	if ignore, _ := strconv.ParseInt(ignoredUsers, 10, 64); ignore == abandonedUser {
		log.Println("(ignored)")
		return
	}
	unfollowers = make([]int64, 0)

	if prevUf == nil || prevUf.Followers == nil {
		log.Println("DiffFollowers: no old followers")
		return
	}
	if newUf == nil || newUf.Followers == nil {
		log.Println("DiffFollowers: no new followers")
		return
	}
	fOld := prevUf.Followers
	fNew := newUf.Followers

	diff := len(fOld) - len(fNew)
	if diff > maxUnfollows {
		log.Fatalf("too many unfollows for user %v: %d > %d",
			abandonedUser, diff, maxUnfollows)
	}

	newMap := map[int64]bool{}
	for _, uid := range fNew {
		newMap[uid] = true
	}

	// We don't care about new followers, only missing ones.
	for _, unfollower := range fOld {
		if unfollower < 184 {
			log.Println("ERROR while comparing user ", strconv.FormatInt(abandonedUser, 10))
			log.Println("ERROR: bogus uid found in old database: ", unfollower)
			//panic("bogus uid" + strconv.Itoa64(uid.(int64)))
			c.db.Reconnect()
			continue
		}
		if _, ok := newMap[unfollower]; !ok {
			if ignore, _ := strconv.ParseInt(ignoredUsers, 10, 64); ignore == unfollower {
				log.Println("(ignored)")
				continue
			}
			unfollowers = append(unfollowers, unfollower)
		}
	}
	return
}

// Notify user and mark unfollow in the database.
func (c *FollowersCrawler) ProcessUnfollow(abandonedUser int64, unfollower int64) (err error) {
	// TODO: Remove after we start caching screen_name => id data.
	if dryRunMode || !notifyUsers {
		return
	}

	if c.db.GetWasUnfollowNotified(abandonedUser, unfollower) {
		log.Println("already notified. ignoring")
		return
	}
	if err = c.NotifyUnfollower(abandonedUser, unfollower); err != nil {
		return err
	}
	if err = c.db.MarkUnfollowNotified(abandonedUser, unfollower); err != nil {
		return err
	}
	return
}

func (c *FollowersCrawler) NotifyUnfollower(abandonedUser, unfollower int64) (err error) {
	abandonedName, err := c.getUserName(abandonedUser)
	if err != nil {
		log.Printf("c.getUserName(abandonedUser) err: %v", err)
		return
	}
	unfollowerName, err := c.getUserName(unfollower)
	if err != nil {
		log.Printf("c.getUserName(unfollower) err: %v", err)
		return
	}
	if dryRunMode || !notifyUsers {
		return
	}
	return c.tw.NotifyUnfollower(abandonedName, unfollowerName)
}

func (c *FollowersCrawler) FollowUser(uid int64) (err error) {
	if dryRunMode {
		return
	}
	if isPending, _ := c.db.GetIsFollowingPending(uid); isPending {
		// Already trying to follow user. Skipping follow request.
		return
	}
	if err = c.tw.FollowUser(uid); err == nil {
		c.db.MarkPendingFollow(uid)
	}
	return
}
