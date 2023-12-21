// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"github.com/dr2chase/gc-lsp-tools/prof"
	"github.com/google/pprof/profile"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "%s whatever.prof reads a pprof protobuf file and prints the contents\n", os.Args[0])
		os.Exit(1)
	}
	fname := os.Args[1]
	f, err := os.Open(fname)
	if err != nil {
		panic(err)
	}

	p1, countIndex, total := prof.FileToSortedProfile(f, 1)

	line2String := func(line *profile.Line) string {
		return fmt.Sprintf("%s:%d", line.Function.Filename, line.Line)
	}

	for _, s := range p1.Sample {
		c := float64(s.Value[countIndex]) / total
		lines := s.Location[0].Line
		l := line2String(&lines[0])
		for i := 1; i < len(lines); i++ {
			l = line2String(&lines[i]) + "\n\t" + l // Reverse the order, outermost first.
		}
		fmt.Printf("%5.2f, %s\n", 100*c, l)
	}
}
