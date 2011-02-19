package main

import "javaitarde"
import "testing"
import __regexp__ "regexp"

var tests = []testing.InternalTest{
	{"javaitarde.TestMongo", javaitarde.TestMongo},
}
var benchmarks = []testing.InternalBenchmark{ //
}

func main() {
	testing.Main(__regexp__.MatchString, tests)
	testing.RunBenchmarks(__regexp__.MatchString, benchmarks)
}
