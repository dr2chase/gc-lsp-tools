// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"github.com/dr2chase/gc-lsp-tools/lsp"
	"github.com/dr2chase/gc-lsp-tools/prof"
	"math"
	"net/url"
	"os"
	"runtime/pprof"
	"strings"
)

var pwd string = os.Getenv("PWD")
var goroot string = os.Getenv("GOROOT")
var gopath string = os.Getenv("GOPATH")
var home string = os.Getenv("HOME")
var strip bool

func shorten(s string) string {
	if !strip {
		return s
	}
	hasPrefix := func(s, prefix string) bool {
		return prefix != "" && strings.HasPrefix(s, prefix)
	}
	switch {
	case hasPrefix(s, pwd):
		return "$PWD" + s[len(pwd):]
	case hasPrefix(s, goroot):
		return "$GOROOT" + s[len(goroot):]
	case hasPrefix(s, gopath):
		return "$GOPATH" + s[len(gopath):]
	case hasPrefix(s, home):
		return "$HOME" + s[len(home):]
	}
	return s
}

// gclsp_prof lspdir profile1 [ profile2 ... ]
//
func main() {
	verbose := false
	before := int64(2)
	after := int64(1)
	cpuprofile := ""
	threshold := 1.0

	flag.BoolVar(&verbose, "v", verbose, "Spews information about profiles read and lsp files")
	flag.Int64Var(&before, "b", before, "Include log entries this many lines before a profile hot spot")
	flag.Int64Var(&after, "a", after, "Include log entries this many lines after a profile hot spot")
	flag.StringVar(&cpuprofile, "cpuprofile", cpuprofile, "Record a cpu profile in this file")
	flag.Float64Var(&threshold, "t", threshold, "Threshold percentage below which profile entries will be ignored")
	flag.BoolVar(&strip, "s", strip, "Shorten file names by substituting PWD, GOROOT, GOPATH, HOME")

	usage := func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			`
%s LspDir Profile1 [ Profile2 ... ] reads the supplied cpu profiles to
determine the hotspots in an application, then reads the compiler logging
information in LspDir to match missed optimizations against hotspots.
`, os.Args[0])
	}

	flag.Usage = usage

	flag.Parse()

	if cpuprofile != "" {
		file, _ := os.Create(cpuprofile)
		pprof.StartCPUProfile(file)
		defer func() {
			pprof.StopCPUProfile()
			file.Close()
		}()
	}

	args := flag.Args()
	if len(args) < 2 {
		usage()
		os.Exit(1)
	}
	lspDir := args[0]
	profiles := args[1:]

	// pi, err := prof.FromTextOutput(profiles)
	pi, err := prof.FromProtoBuf(profiles)

	if len(pi) == 0 {
		return
	}

	if verbose {
		for _, p := range pi {
			if p.FlatPercent >= threshold {
				fmt.Printf("%f%%, %s:%d\n", p.FlatPercent, p.FileLine[0].SourceFile, p.FileLine[0].Line)
			}
		}
	}

	byFile := make(map[string]*lsp.CompilerDiagnostics)
	err = lsp.ReadAll(lspDir, &byFile, verbose)
	if err != nil {
		panic(err)
	}

	near := func(d *lsp.Diagnostic, line int64) bool {
		diag := int64(d.Range.Start.Line)
		return line-before <= diag && diag <= line+after
	}

	for _, p := range pi {
		if p.FlatPercent >= threshold {
			cd := byFile[p.FileLine[0].SourceFile]
			if cd != nil && len(cd.Diagnostics) > 0 {
				for _, d := range cd.Diagnostics {
					if d.Code == "inlineCall" { // Don't want to see this.
						continue
					}
					for i, fl := range p.FileLine {
						p.FileLine[i].SourceFile = shorten(fl.SourceFile)
					}
					fl := p.FileLine[0]
					if near(d, fl.Line) {
						nearby := "nearby "
						if int64(d.Range.Start.Line) == p.FileLine[0].Line {
							nearby = ""
						}
						if d.Message != "" {
							fmt.Printf("%5.1f%%, %s:%d :: %s, %s (at %sline %d)\n", p.FlatPercent, fl.SourceFile, fl.Line, d.Code, d.Message, nearby, d.Range.Start.Line)
						} else {
							fmt.Printf("%5.1f%%, %s:%d :: %s (at %sline %d)\n", p.FlatPercent, fl.SourceFile, fl.Line, d.Code, nearby, d.Range.Start.Line)
						}
						profileInlines := p.FileLine[1:]
						var diagnosticInlines []prof.FileLine
						for _, ri := range d.RelatedInformation {
							if ri.Message != "inlineLoc" {
								// don't bother with additional messages, e.g. escape explanations
								break
							}
							uri := string(ri.Location.URI)

							if strings.HasPrefix(uri, "file://") {
								s, err := url.PathUnescape(uri[7:])
								if err != nil { // TODO should we say something?
									s = uri[7:]
								}
								uri = s
							}
							diagnosticInlines = append(diagnosticInlines, prof.FileLine{
								SourceFile: shorten(uri),
								Line:       int64(ri.Location.Range.Start.Line),
							})
						}

						// Handle inlining of either profile or diagnostic
						var profLocs []string
						var diagLocs []string
						for i, j := 0, 0; i < len(profileInlines) || j < len(diagnosticInlines); i, j = i+1, j+1 {
							if i < len(profileInlines) {
								il := profileInlines[i]
								profLocs = append(profLocs, fmt.Sprintf("%s:%d", il.SourceFile, il.Line))
							} else {
								profLocs = append(profLocs, "")
							}
							if j < len(diagnosticInlines) {
								il := diagnosticInlines[j]
								diagLocs = append(diagLocs, fmt.Sprintf("%s:%d", il.SourceFile, il.Line))
							} else {
								diagLocs = append(diagLocs, "")
							}
						}
						if len(profLocs) == 0 {
							// no inlining, never mind
							continue
						}
						// make them all, respectively, the same length
						makeSameLengths := func(ss []string) {
							max := 0
							min := math.MaxInt32
							for _, s := range ss {
								if len(s) > max {
									max = len(s)
								}
								if len(s) < min {
									min = len(s)
								}
							}
							pad := strings.Repeat(" ", max-min)
							for i, s := range ss {
								ss[i] = s + pad[0:max-len(s)]
							}
						}
						makeSameLengths(profLocs)
						makeSameLengths(diagLocs)
						for i := 0; i < len(profLocs); i++ {
							fmt.Printf("\t%s :: %s\n", profLocs[i], diagLocs[i])
						}
					}
				}
			}
		}
	}
}
