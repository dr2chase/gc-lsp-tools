// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"github.com/dr2chase/gc-lsp-tools/diffcov"
	"github.com/dr2chase/gc-lsp-tools/reuse"
	"os"
)

// diffcov whatever.diff
func main() {
	var verbose reuse.Count
	var coverprofile string
	var diffDir string

	flag.Var(&verbose, "v", "Says more")
	flag.StringVar(&coverprofile, "c", coverprofile, "name of test -coverprofile output file")
	flag.StringVar(&diffDir, "d", diffDir, "diff directory root (typically git repo root)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
'%s diffFile' reports the new statements in diffFile that do not appear in the coverprofile.
If there is no coverprofile, it reports all the new statements.
`, os.Args[0])
	}

	flag.Parse()

	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}
	diffs := flag.Args()[0]

	diffcov.DoDiffs(diffs, coverprofile, diffDir, verbose)
}
