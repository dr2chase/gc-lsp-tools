// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prof

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/google/pprof/profile"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type FileLine struct {
	SourceFile string
	Line       int64
}

type ProfileItem struct {
	FlatPercent float64
	FileLine    []FileLine
}

func numberStuff(s string) (n float64, stuff string, rest string) {
	s = strings.TrimSpace(s)
	firstNN := -1
	last := len(s)
	for i, r := range s {
		if !unicode.IsDigit(r) && r != '.' && firstNN == -1 {
			firstNN = i
		}
		if !unicode.IsSpace(r) {
			continue
		}
		last = i
		break
	}
	stuff = s[firstNN:last]
	n, err := strconv.ParseFloat(s[0:firstNN], 64)
	if err != nil {
		panic(err)
	}
	rest = s[last:]
	return
}

func nextField(s string) (stuff string, rest string) {
	s = strings.TrimSpace(s)
	last := len(s)
	for i, r := range s {
		if !unicode.IsSpace(r) {
			continue
		}
		last = i
		break
	}
	stuff = s[0:last]
	rest = s[last:]
	return
}

func scale(n float64, unit string) float64 {
	switch unit {
	case "ks":
		return n * 1000
	case "ms":
		return n / 1000
	case "cs":
		return n / 100
	case "ds":
		return n / 10
	case "us", "µs":
		return n / 1000000
	case "ns":
		return n / 1000000000
	}
	return n
}

func lineToProfileItem(s string) *ProfileItem {
	s0 := s
	pi := ProfileItem{}
	//	flat  flat%   sum%        cum   cum%   package.type.methfunc sourcefile:Line
	//	0.03s  2.26% 96.99%      0.03s  2.26%  runtime.asyncPreempt /Users/drchase/work/go/src/runtime/preempt_amd64.s:7
	_, stuff, s := numberStuff(s) // was n
	if s == "" {
		panic("Unexpected Line " + s0)
	}
	// pi.flatSeconds = scale(n, stuff)

	pi.FlatPercent, stuff, s = numberStuff(s)
	if s == "" || stuff != "%" {
		panic("Unexpected Line " + s0 + " stuff= " + stuff)
	}

	_, stuff, s = numberStuff(s) // was sumPercent
	if s == "" || stuff != "%" {
		panic("Unexpected Line " + s0)
	}

	_, stuff, s = numberStuff(s) // was n
	if s == "" {
		panic("Unexpected Line " + s0)
	}
	// pi.cumulativeSeconds = scale(n, stuff)

	_, stuff, s = numberStuff(s) // was pi.cumulativePercent
	if s == "" || stuff != "%" {
		panic("Unexpected Line " + s0)
	}

	_, s = nextField(s) // was pi.methodOrFunc
	if s == "" {
		panic("Unexpected Line " + s0)
	}

	fileLine, s := nextField(s)
	// TODO it might say (inline) which needs to be dealt with anyway.
	//if s != "" {
	//	panic("Unexpected Line " + s0)
	//}

	colon := strings.LastIndexByte(fileLine, ':')
	if colon == -1 {
		panic("Unexpected Line " + s0)
	}
	var fl FileLine
	fl.SourceFile = fileLine[0:colon]
	line, err := strconv.ParseInt(fileLine[colon+1:], 10, 32)
	if err != nil {
		panic(err)
	}
	fl.Line = line
	pi.FileLine = append(pi.FileLine, fl)
	return &pi
}

func FileToSortedProfile(f *os.File) (*profile.Profile, int, float64) {
	p1, err := profile.Parse(f)
	if err != nil {
		panic(err)
	}

	countIndex := -1
	for i, t := range p1.SampleType {
		if t.Type == "samples" {
			countIndex = i
			break
		}
	}

	countTotal := 0.0
	for _, s := range p1.Sample {
		countTotal += float64(s.Value[countIndex])
	}

	sort.Slice(p1.Sample, func(i, j int) bool {
		return p1.Sample[i].Value[countIndex] < p1.Sample[j].Value[countIndex]
	})
	return p1, countIndex, countTotal
}

func FromProtoBuf(profiles []string) ([]*ProfileItem, error) {
	var pi []*ProfileItem
	tempFile, err := ioutil.TempFile("", "profile.*.pb.gz")
	if err != nil {
		panic(err)
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempFile.Name())
	}()
	cmdArgs := []string{"tool", "pprof", "-proto", "-lines", "-flat", "-output", tempFile.Name()}
	cmdArgs = append(cmdArgs, profiles...)
	cmd := exec.Command("go", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		m := ""
		for _, s := range cmdArgs {
			m = m + " " + s
		}
		fmt.Printf("Failed to run go%s\n", m)
		fmt.Println(string(out))
		fmt.Println(err)
		return nil, nil
	}

	p, countIndex, countTotal := FileToSortedProfile(tempFile)

	for _, s := range p.Sample {
		c := float64(s.Value[countIndex]) / countTotal
		lines := s.Location[0].Line
		l := len(lines)
		fileLines := make([]FileLine, l)
		for i, line := range lines {
			fileLines[l-i-1] = FileLine{
				SourceFile: line.Function.Filename,
				Line:       line.Line,
			}
		}
		pi = append(pi, &ProfileItem{
			FlatPercent: 100 * c,
			FileLine:    fileLines,
		})
	}
	return pi, nil
}

func FromTextOutput(profiles []string) ([]*ProfileItem, error) {
	var pi []*ProfileItem
	cmdArgs := []string{"tool", "pprof", "-text", "-lines", "-flat"}
	cmdArgs = append(cmdArgs, profiles...)
	cmd := exec.Command("go", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		m := ""
		for _, s := range cmdArgs {
			m = m + " " + s
		}
		fmt.Printf("Failed to run go%s\n", m)
		fmt.Println(string(out))
		fmt.Println(err)
		return nil, nil
	}
	r := bufio.NewReader(bytes.NewBuffer(out))
	skippingLines := true
	for i := 1; err != io.EOF; i++ {
		var s string
		s, err = r.ReadString('\n')
		if err != nil && err != io.EOF {
			panic(err)
		}
		if skippingLines {
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "flat") {
				skippingLines = false
			}
			continue
		}
		if s == "" {
			continue
		}
		pi = append(pi, lineToProfileItem(s))
	}
	return pi, err
}
