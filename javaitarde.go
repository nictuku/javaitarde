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
	javaitarde "github.com/nictuku/javaitarde/crawl"
	"log"
)

var (
	hubUserUid      int64
	runContinuously bool
)

func init() {
	flag.Int64Var(&hubUserUid, "hubuid", 217554981,
		"Uid of our user, whose followers we want to track for unfollows.")
}

func main() {
	flag.Parse()

	crawler := javaitarde.NewFollowersCrawler()
	if err := crawler.FindOurUsers(hubUserUid); err != nil {
		log.Fatal("crawler.FindOurUsers:", err)
	}
	if err := crawler.GetAllUsersFollowers(); err != nil {
		log.Fatal("crawler.GetAllUsersFollowers:", err)
	}
}
