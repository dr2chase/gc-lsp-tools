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
	FlatTotal   float64
	FileLine    []FileLine
}

type ValueType struct {
	Type string // cpu, wall, inuse_space, etc
	Unit string // seconds, nanoseconds, bytes, etc

	typeX int64
	unitX int64
}

// FileToSortedProfile reads a file containing possibly compressed
// protobuf form of pprof data, and returns the profile.Profile
// contained with, plus the Sample[*].Value index of the sample
// count and the sum of those counts.
func FileToSortedProfile(f *os.File, verbose int) (*profile.Profile, int, float64) {
	p1, err := profile.Parse(f)
	if err != nil {
		panic(err)
	}

	countIndex := -1
	for i, t := range p1.SampleType {
		if verbose > 1 {
			fmt.Fprintf(os.Stderr, "Sample type %d=%s\n", i, t.Type)
		}
		if t.Type == "samples" || t.Type == "alloc_space" {
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

type flsMap map[FileLine]struct {
	index int
	il    flsMap
}

func (m flsMap) put(s []FileLine, index int) {
	x := m[s[0]]
	if len(s) == 1 {
		x.index = index
		m[s[0]] = x
		return
	}
	if x.il == nil {
		t := make(flsMap)
		x.il = t
	}
	x.il.put(s[1:], index)
	m[s[0]] = x
}

func (m flsMap) get(s []FileLine) (index int, ok bool) {
	x, xok := m[s[0]]
	if !xok {
		return -1, false
	}
	if len(s) == 1 {
		return x.index, true
	}
	if x.il == nil {
		return -1, false
	}
	return x.il.get(s[1:])

}

// FromProtoBuf runs go tool pprof on the supplied profiles to generate
// the (-flat, -lines) protobuf output, and then processes that protobuf
// to yield a sorted profile of sample percentages and sample locations.
// If combine is true, samples with equal file(s) and line(s) are merged.
func FromProtoBuf(profiles []string, combine, innermost bool, verbose int) ([]*ProfileItem, error) {
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

	p, countIndex, countTotal := FileToSortedProfile(tempFile, verbose)

	flsmap := make(flsMap)

	var pi []*ProfileItem

	for _, s := range p.Sample {
		if len(s.Location) == 0 {
			continue
		}
		val := float64(s.Value[countIndex])
		c := val / countTotal
		lines := s.Location[0].Line
		l := len(lines)
		if l == 0 {
			continue
		}
		var fileLines []FileLine
		if innermost {
			fileLines = []FileLine{{
				SourceFile: lines[0].Function.Filename,
				Line:       lines[0].Line,
			}}
		} else {
			fileLines = make([]FileLine, l, l)
			for i, line := range lines {
				fileLines[l-i-1] = FileLine{
					SourceFile: line.Function.Filename,
					Line:       line.Line,
				}
			}
		}

		if combine {
			i, ok := flsmap.get(fileLines)
			// if fileLines[len(fileLines)-1].SourceFile == "/Users/drchase/work/go/src/cmd/internal/obj/data.go" {
			// 	fmt.Fprintf(os.Stderr, "fl[0]=%v, len(fl)=%v, i=%v, ok=%v, len(pi)=%v\n", fileLines[0].SourceFile, len(fileLines), i, ok, len(pi))
			// }
			if ok {
				pi[i].FlatTotal += val
				pi[i].FlatPercent += 100 * c
				continue
			}
			flsmap.put(fileLines, len(pi))
		}

		pi = append(pi, &ProfileItem{
			FlatPercent: 100 * c,
			FlatTotal:   val,
			FileLine:    fileLines,
		})
	}

	if combine {
		sort.Slice(pi, func(i, j int) bool { return pi[i].FlatPercent < pi[j].FlatPercent })
	}
	return pi, nil
}
