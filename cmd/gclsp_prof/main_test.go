// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var cwd string

func init() {
	var err error
	cwd, err = os.Getwd()
	if err != nil {
		panic(err)
	}
}

var (
	keep = flag.Bool("keep", false, "do not delete the temporary directory")
	here = flag.Bool("here", false, "run the test in this directory")
)

// replaceEnv returns a new environment derived from env
// by removing any existing definition of ev and adding ev=evv.
func replaceEnv(env []string, ev string, evv string) []string {
	evplus := ev + "="
	var newenv []string
	for _, v := range env {
		if !strings.HasPrefix(v, evplus) {
			newenv = append(newenv, v)
		}
	}
	newenv = append(newenv, evplus+evv)
	return newenv
}

func trim(s, cwd string, needsDotSlash bool) string {
	if s == cwd {
		return "."
	}
	if strings.HasPrefix(s, cwd+"/") {
		s = s[1+len(cwd):]
	} else if strings.HasPrefix(s, cwd+string(filepath.Separator)) {
		s = s[len(cwd+string(filepath.Separator)):]
	} else {
		return s
	}
	if needsDotSlash {
		s = "." + string(filepath.Separator) + s
	}
	return s
}

// runCmd wraps running a command with an error check,
// failing the test if there is an error.  The combined
// output is returned.
func runCmd(cmd *exec.Cmd, t *testing.T) []byte {
	line := "("
	wd := cwd
	if cmd.Dir != "" && cmd.Dir != "." && cmd.Dir != cwd {
		wd = cmd.Dir
		line += " cd " + trim(cmd.Dir, cwd, false) + " ; "
	}
	// line += trim(cmd.Path, wd)
	for i, s := range cmd.Args {
		line += " " + trim(s, wd, i == 0)
	}
	line += " )"
	t.Logf("%s", line)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n\n%v", string(out), err)
		t.FailNow()
	}
	return out
}

func TestIt(t *testing.T) {
	testdir := path.Join(cwd, "testdata")
	if !*here {
		var err error
		testdir, err = ioutil.TempDir("", "GcLspProfTest")
		if err != nil {
			panic(err)
		}
		if !*keep {
			defer os.RemoveAll(testdir) // clean up
		}
	}

	binary := path.Join(testdir, "foo.exe")
	gclsp_prof := path.Join(testdir, "gclsp_prof.exe")
	lspdir := path.Join(testdir, "gclsp")

	cmd := exec.Command("go", "build", "-o", binary, "-gcflags=-json=0,"+lspdir, "testdata/foo.go")
	_ = runCmd(cmd, t)

	cmd = exec.Command("go", "build", "-o", gclsp_prof, ".")
	_ = runCmd(cmd, t)

	cmd = exec.Command(binary)
	cmd.Dir = testdir
	_ = runCmd(cmd, t)

	cmd = exec.Command(gclsp_prof, "-a=0", "-b=0", "-t=4.0", "-s", "./gclsp", "foo.prof")
	cmd.Dir = testdir
	cmd.Env = replaceEnv(os.Environ(), "PWD", testdir)
	out := string(runCmd(cmd, t))
	t.Logf("\n%s", out)

	split := strings.Split(out, "\n")

	if len(split) > 9 {
		split = split[len(split)-9:]
	}
	matchREs := []string{
		".*/foo[.]go:94 :: isInBounds [(]at line 94[)]",
		".*/foo[.]go:20 :: [$].*/foo[.]go:20",
		".*/foo[.]go:94 :: isInBounds [(]at line 94[)]",
		".*/foo[.]go:20 :: [$].*/foo[.]go:20",
		".*/foo[.]go:38 :: isInBounds [(]at line 38[)]",
		".*/foo[.]go:20 :: [$].*/foo[.]go:16",
		".*/foo[.]go:38 :: isInBounds [(]at line 38[)]",
		".*/foo[.]go:20 :: [$].*/foo[.]go:16",
	}
	for i, s := range split {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		re := matchREs[i]
		match, err := regexp.MatchString(re, s)
		if err != nil {
			panic(err)
		}
		if !match {
			t.Errorf("%s failed to match %s", s, re)
		} else {
			t.Logf("Line %d matched regexp %s", i, re)
		}
	}
}
