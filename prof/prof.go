// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prof

import (
	"fmt"
	"github.com/google/pprof/profile"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
)

type FileLine struct {
	SourceFile string
	Line       int64
}

// ProfileItem represents one sample location, and provides the percentage
// of the total and the outermost-first slice of file-and-line positions.
type ProfileItem struct {
	FlatPercent float64
	FileLine    []FileLine
}

// FileToSortedProfile reads a file containing possibly compressed
// protobuf form of pprof data, and returns the profile.Profile
// contained with, plus the Sample[*].Value index of the sample
// count and the sum of those counts.
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

// FromProtoBuf runs go tool pprof on the supplied profiles to generate
// the (-flat, -lines) protobuf output, and then processes that protobuf
// to yield a sorted profile of sample percentages and sample locations.
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
