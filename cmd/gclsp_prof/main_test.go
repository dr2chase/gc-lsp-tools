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

func runThing(t *testing.T, thing string) (split []string, doafter func()) {
	testdir := path.Join(cwd, "testdata", thing)
	if !*here {
		var err error
		testdir, err = ioutil.TempDir("", "GcLspProfTest")
		if err != nil {
			panic(err)
		}
		if !*keep {
			doafter = func() { os.RemoveAll(testdir) } // clean up
		}
	}

	t.Logf("%s", string(runCmd(exec.Command("go", "version"), t)))

	// Important files
	binary := path.Join(testdir, thing+".exe")
	gclsp_prof := path.Join(testdir, "gclsp_prof.exe")
	lspdir := path.Join(testdir, thing+".lspdir")

	cmd := exec.Command("go", "build", "-o", binary, "-a", "-gcflags=-d=loopvar=3 -json=0,"+lspdir, "testdata/"+thing+"/"+thing+".go")
	t.Logf("%s", string(runCmd(cmd, t)))

	cmd = exec.Command("go", "build", "-o", gclsp_prof, ".")
	_ = runCmd(cmd, t)

	cmd = exec.Command(binary)
	cmd.Dir = testdir
	_ = runCmd(cmd, t)

	cmd = exec.Command(gclsp_prof, "-a=1", "-b=1", "-t=10.0", "./"+thing+".lspdir", thing+".prof")
	cmd.Dir = testdir
	cmd.Env = replaceEnv(os.Environ(), "PWD", testdir)
	out := string(runCmd(cmd, t))
	t.Logf("\n%s", out)

	split = strings.Split(out, "\n")

	return
}

func TestBar(t *testing.T) {
	split, doafter := runThing(t, "bar")
	if doafter != nil {
		defer doafter()
	}
	for _, s := range split {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		t.Logf("%s", s)
	}
}

func TestFoo(t *testing.T) {

	split, doafter := runThing(t, "foo")
	if doafter != nil {
		defer doafter()
	}

	// This can fail if the profiles are far from expected values, which might happen sometimes or on some architectures.

	// The working directory can be ID'd in several ways
	pwdre := "[$](PWD|(GOPATH|HOME/.*)/src/github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof/testdata/foo)"
	matchREs := []string{
		" *[0-9]+[.][0-9]+%, " + pwdre + "/foo[.]go:[0-9]+[)]",
		" *isInBounds [(]at later line [0-9]+[)]",
		" *[(]inline[)] " + pwdre + "/foo[.]go:[0-9]+",
		" *[0-9]+[.][0-9]+%, " + pwdre + "/foo[.]go:[0-9]+[)]",
		" *isInBounds [(]at earlier line [0-9]+[)]",
		" *[(]inline[)] " + pwdre + "/foo[.]go:[0-9]+",
		" *isInBounds [(]at line [0-9]+[)]",
		" *[(]inline-nearby[)] " + pwdre + "/foo[.]go:[0-9]+",
		" *isInBounds [(]at later line [0-9]+[)]",
		" *[(]inline[)] " + pwdre + "/foo[.]go:[0-9]+",
	}

	expectedTailLen := len(matchREs) + 1 // Last line is blank.
	if len(split) > expectedTailLen {
		split = split[len(split)-expectedTailLen:]
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

// trim shortens s to be relative to cwd, if possible.
// needsDotSlash indicates that s is something like a command
// and must contain at least one path separator (because "." is
// by sensible default not on paths).
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
