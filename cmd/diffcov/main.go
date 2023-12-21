// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dr2chase/gc-lsp-tools/diffcov"
	"github.com/dr2chase/gc-lsp-tools/reuse"
)

func fail(format string, args ...any) {
	flag.Usage()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

// diffcov whatever.diff
func main() {
	var verbose reuse.Count
	var coverprofile string
	var diffDir string
	var diffFile string
	var modDir string
	var strip int
	var staged bool
	var showTested bool

	flag.Var(&verbose, "v", "Says more and more")
	flag.StringVar(&coverprofile, "c", coverprofile, "name of test -coverprofile output file")
	flag.StringVar(&diffFile, "d", diffFile, "name of (git) diff output file")
	flag.StringVar(&diffDir, "D", diffDir, "diff directory root (typically parent of git repo root)")
	flag.StringVar(&modDir, "M", modDir, "directory containing go.mod")
	flag.IntVar(&strip, "S", strip, "number of leading directories to strip from files in diff (useful w/ packages differently named from directory)")
	flag.BoolVar(&staged, "staged", staged, "if run no-args, use git --staged to obtain diff")
	flag.BoolVar(&staged, "cached", staged, "if run no-args, use git --staged to obtain diff")
	flag.BoolVar(&showTested, "t", showTested, "also show the tested lines")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
'%[1]s [options] -c coverprofile [-d] diffFile' reports the new statements in diffFile that do not appear in the coverprofile.
'%[1]s [options] [-d] diffFile' reports all the new statements in diffFile.
'%[1]s [no options]' will attempt to run 'git diff' and 'go test -coverprofile' to automatically generate -c/-d files.
If -M, -D, -S are not provided, %[1]s searches in parent directories for clues.
`, os.Args[0])
	}

	flag.Parse()

	var err error
	var diffBytes []byte

	if len(flag.Args()) != 1 && diffFile == "" {
		// With no args, attempt to automatically do the right thing; run diff, run the test, use those.
		gitArgs := []string{"diff"}
		if staged {
			gitArgs = append(gitArgs, "--staged")
		}
		gitCmd := exec.Command("git", gitArgs...)
		if verbose > 0 {
			fmt.Fprintf(os.Stderr, "Running git, gitArgs=%v\n", gitArgs)
		}
		diffBytes, err = gitCmd.CombinedOutput()
		if err != nil {
			fail("%s\nfailed to run git diff, err=%v, output was\n", string(diffBytes), err)
		}
		if len(diffBytes) == 0 {
			fail("git diff return empty output, perhaps there is a problem with the directory or the flags?\n")
		}
		coverDir, err := os.MkdirTemp("", "diffcov")
		if err != nil {
			fail("failed to create temporary dir, err=%v\n", err)
		}

		coverprofile = filepath.Join(coverDir, "coverprofile.out")

		testArgs := []string{"test", "-coverprofile", coverprofile, "."}
		testCmd := exec.Command("go", testArgs...)
		if verbose > 0 {
			fmt.Fprintf(os.Stderr, "Running go test, args=%v\n", testArgs)
		}
		coverOut, err := testCmd.CombinedOutput()
		if err != nil {
			fail("%s\nfailed to run go test ..., err=%v\n", string(coverOut), err)
		}
		if verbose > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", string(coverOut))
		}
	} else {
		diffFile = flag.Args()[0]
		diffBytes, err = os.ReadFile(diffFile)
		if err != nil {
			fail("could not read diff from %s, error was %v\n", diffFile, err)
		}
	}

	diffcov.DoDiffs(diffBytes, coverprofile, diffDir, modDir, strip, verbose, showTested)
}
