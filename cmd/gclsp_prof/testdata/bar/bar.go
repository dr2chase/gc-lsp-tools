// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"runtime/pprof"
)

type thing struct {
	i int
	p *int
	j int
	q *int
}

var sink map[*thing]thing = make(map[*thing]thing)

func aloop(N int) {
	var th thing

	for t := th; t.i < N; t.i++ {
		for k, v := range sink {
			if k.i != v.i {
				sink[k] = *k
				break
			}
		}
		sink[&t] = t
	}
}

func Do(N int) {
	cpuprofile := "bar.prof"
	if cpuprofile != "" {
		file, _ := os.Create(cpuprofile)
		pprof.StartCPUProfile(file)
		defer func() {
			pprof.StopCPUProfile()
			file.Close()
		}()
	}
	aloop(N)
}

func main() {
	Do(64 * 1024)
}
