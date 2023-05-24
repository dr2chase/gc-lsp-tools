// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"time"
)

var countLimit int64 = 0
var timeLimit time.Duration
var verbose bool

var fakeRunTime time.Duration = time.Duration(time.Second)
var fakeFailRate int64

func main() {
	flag.Int64Var(&countLimit, "n", countLimit, "Number of times to repeat a command")
	flag.DurationVar(&timeLimit, "t", timeLimit, "Amount of time to repeat a command")
	flag.BoolVar(&verbose, "v", verbose, "Verbose -- print command output")

	flag.DurationVar(&fakeRunTime, "T", fakeRunTime, "For testing, as a flaky command, the time to take")
	flag.Int64Var(&fakeFailRate, "F", fakeFailRate, "For testing, as a flaky command, fail 1/this number often")

	flag.Parse()

	if fakeFailRate != 0 {
		// act like a command and fail at this rate, after taking some time.
		seed := time.Now().UnixNano()
		time.Sleep(fakeRunTime)
		rand.Seed(seed)
		if rand.Int63n(fakeFailRate) == 0 {
			fmt.Printf("FAIL\n")
			os.Exit(1)
		}
		fmt.Printf("PASS\n")
		os.Exit(0)
	}

	rest := flag.Args()
	if rest[0] == "--" {
		rest = rest[1:]
	}

	var timer *time.Timer
	doneChan := make(chan int, 1)
	if timeLimit > 0 {
		timer = time.AfterFunc(timeLimit, func() { doneChan <- 1 })
	}
	done := func() bool {
		if timer == nil {
			return false
		}
		select {
		case <-doneChan:
			return true
		default:
		}
		return false
	}

	if countLimit == 0 {
		countLimit = math.MaxInt64 - 1
	}

	for i := int64(1); i <= countLimit && !done(); i++ {
		cmd := exec.Command(rest[0], rest[1:]...)
		output, err := cmd.CombinedOutput()
		if verbose {
			fmt.Fprintf(os.Stdout, "%s\n", string(output))
		}
		if err != nil {
			os.Exit(1)
		}
	}

	os.Exit(0)
}
