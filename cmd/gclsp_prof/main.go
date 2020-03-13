// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"github.com/dr2chase/gc-lsp-tools/lsp"
	"github.com/dr2chase/gc-lsp-tools/prof"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strings"
)

var pwd string = os.Getenv("PWD")
var goroot string = os.Getenv("GOROOT")
var gopath string = os.Getenv("GOPATH")
var home string = os.Getenv("HOME")

type abbreviation struct{ substring, replace string }

var shortenEVs string = "PWD,GOROOT,GOPATH,HOME"
var abbreviations []abbreviation

var bench string
var keep string

// gclsp_prof [-v] [-a=n] [-b=n] [-t=f.f] [-s] [-cpuprofile=file]  lspdir profile1 [ profile2 ... ]
// Produces a summary of optimizations (if any) that were not or could not be applied at hotspots in the profile.
func main() {
	verbose := false
	before := int64(0)
	after := int64(0)
	explain := false
	cpuprofile := ""
	threshold := 1.0

	flag.BoolVar(&verbose, "v", verbose, "Spews information about profiles read and lsp files")
	flag.BoolVar(&explain, "e", explain, "Also supply \"explanations\"")
	flag.Int64Var(&before, "b", before, "Include log entries this many lines before a profile hot spot")
	flag.Int64Var(&after, "a", after, "Include log entries this many lines after a profile hot spot")
	flag.StringVar(&cpuprofile, "cpuprofile", cpuprofile, "Record a cpu profile in this file")
	flag.Float64Var(&threshold, "t", threshold, "Threshold percentage below which profile entries will be ignored")
	flag.StringVar(&shortenEVs, "s", shortenEVs, "Environment variables used to abbreviate file names in output")
	flag.StringVar(&bench, "bench", bench, "Run 'bench' benchmarks in current directory and reports hotspot(s). Passes -bench=whatever to go test, as well as arguments past --")
	flag.StringVar(&keep, "keep", keep, "For -bench, keep the intermedia results in <-keep>.lspdir and <-keep>.prof")

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

	// Assemble abbreviations
	ss := strings.Split(shortenEVs, ",")
	for _, s := range ss {
		s = strings.TrimSpace(s)
		v := os.Getenv(s)
		if v != "" {
			abbreviations = append(abbreviations, abbreviation{substring: v, replace: "$" + s})
		}
	}

	args := flag.Args()

	if bench != "" {
		var cleanup func()
		args, cleanup = runBench(args)
		defer cleanup()
	}

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
					if d.Code == "inlineCall" { // Don't want to see these, they are confusing and eventually removed..
						continue
					}
					for i, fl := range p.FileLine {
						p.FileLine[i].SourceFile = shorten(fl.SourceFile)
					}
					fl := p.FileLine[0]

					if near(d, fl.Line) {
						nearby := ""
						if int64(d.Range.Start.Line) < p.FileLine[0].Line {
							nearby = "earlier "
						}
						if int64(d.Range.Start.Line) > p.FileLine[0].Line {
							nearby = "later "
						}

						// Sort through inlines first to determine if this is an exact match or earlier/later/nearby

						// Pull diagnostic inline location from RelatedInformation
						profileInlines := p.FileLine[1:]
						diagnosticInlines, remainingRelated := inlinesFromRelated(d.RelatedInformation)

						// Match up inlines for profile and diagnostic for "nearness" and printing.
						var profLocs []string
						var diagLocs []string
						for i, j := 0, 0; i < len(profileInlines) || j < len(diagnosticInlines); i, j = i+1, j+1 {
							// Adjust declarations of "nearness", for inlines insensitive to before/after for now.
							if nearby == "" {
								if i < len(profileInlines) && j < len(diagnosticInlines) {
									if diagnosticInlines[j].SourceFile != profileInlines[i].SourceFile {
										nearby = "nearby (inline) " // different files
									} else if diagnosticInlines[j].Line < profileInlines[i].Line {
										nearby = "earlier (inline) "
									} else if diagnosticInlines[j].Line < profileInlines[i].Line {
										nearby = "later (inline) "
									} else {
										// still the same
									}
								} else {
									nearby = "nearby (inline) " // mismatched depths
								}
							}

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

						tab := "        " // Tabs vary

						// Now it's known if it's nearby or not, start printing....
						if d.Message != "" { // Note '%5.1f%%, ' is 8 runes wide
							fmt.Printf("%5.1f%%, %s:%d :: %s, %s (at %sline %d)\n", p.FlatPercent, fl.SourceFile, fl.Line, d.Code, d.Message, nearby, d.Range.Start.Line)
						} else {
							fmt.Printf("%5.1f%%, %s:%d :: %s (at %sline %d)\n", p.FlatPercent, fl.SourceFile, fl.Line, d.Code, nearby, d.Range.Start.Line)
						}

						// Print inline information, as necessary
						if len(profLocs) == 0 {
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
							fmt.Printf("%8s%s :: %s\n", tab, profLocs[i], diagLocs[i])
						}
						// Handle extended "explanations".
						if explain {
							for len(remainingRelated) > 0 {
								fl := fileLineFromRelated(&remainingRelated[0])
								fmt.Printf("%sexplanation :: %s:%d, %s\n", tab, fl.SourceFile, fl.Line, remainingRelated[0].Message)
								diagnosticInlines, remainingRelated = inlinesFromRelated(remainingRelated[1:])
								for _, fl := range diagnosticInlines {
									fmt.Printf("%19s :: %s:%d\n", tab, fl.SourceFile, fl.Line)
								}
							}
						}
					}
				}
			}
		}
	}
}

// fileLineFromRelated returns the normalized file and line number from the
// Location of its DiagnosticRelatedInformation argument.
func fileLineFromRelated(ri *lsp.DiagnosticRelatedInformation) prof.FileLine {
	uri := string(ri.Location.URI)
	if strings.HasPrefix(uri, "file://") {
		s, err := url.PathUnescape(uri[7:])
		if err != nil { // TODO should we say something?
			s = uri[7:]
		}
		uri = s
	}
	return prof.FileLine{
		SourceFile: shorten(uri),
		Line:       int64(ri.Location.Range.Start.Line),
	}
}

// inlinesFromRelated extracts any inline locations from a slice of DiagnosticRelatedInformation
// and returns the file+line for the inlines and the remaining subslice of DiagnosticRelatedInformation.
func inlinesFromRelated(relatedInformation []lsp.DiagnosticRelatedInformation) ([]prof.FileLine, []lsp.DiagnosticRelatedInformation) {
	var diagnosticInlines []prof.FileLine
	for i, ri := range relatedInformation {
		if ri.Message != "inlineLoc" {
			return diagnosticInlines, relatedInformation[i:]
		}
		diagnosticInlines = append(diagnosticInlines, fileLineFromRelated(&ri))
	}
	return diagnosticInlines, nil
}

// runBench runs a benchmark bench (see global) found in the currrent directory,
// with appropriate flags to collect both LSP-encoded compiler diagnostics and
// a cpuprofile.  The returned string contains the names of the diagnostics directory
// and cpuprofile file, and a cleanup function to remove any temporary directories
// created here.
func runBench(testargs []string) (newargs []string, cleanup func()) {
	testdir, err := os.Getwd()
	cleanup = func() {}
	if keep == "" {
		testdir, err = ioutil.TempDir("", "GcLspProfBench")
		if err != nil {
			panic(err)
		}
		cleanup = func() { os.RemoveAll(testdir) }
		abbreviations = append(abbreviations, abbreviation{substring: testdir, replace: "$TEMPDIR"})
		keep = "gclsp-bench"
	}
	// go test -gcflags=all=-json=0,testdir/gclsp -cpuprofile=testdir/test.prof -bench=Bench .
	lsp := filepath.Join(testdir, keep+".lspdir")
	cpuprofile := filepath.Join(testdir, keep+".prof")

	cmdArgs := []string{"test", "-gcflags=all=-json=0," + lsp, "-cpuprofile=" + cpuprofile, "-bench=" + bench, "."}
	cmdArgs = append(cmdArgs, testargs...)
	cmd := exec.Command("go", cmdArgs...)
	out := runCmd(cmd)
	fmt.Printf("%s\n", string(out))
	newargs = []string{lsp, cpuprofile}
	return
}

// runCmd wraps running a command with an error check,
// failing the test if there is an error.  The combined
// output is returned.
func runCmd(cmd *exec.Cmd) []byte {
	line := "("
	wd := pwd
	if cmd.Dir != "" && cmd.Dir != "." && cmd.Dir != pwd {
		wd = cmd.Dir
		line += " cd " + trimCwd(cmd.Dir, pwd, false) + " ; "
	}
	// line += trim(cmd.Path, wd)
	for i, s := range cmd.Args {
		line += " " + trimCwd(s, wd, i == 0)
	}
	line += " )"
	fmt.Printf("%s\n", line)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n\n%v", string(out), err)
		os.Exit(1)
	}
	return out
}

// trim shortens s to be relative to cwd, if possible, and also
// replaces embedded instances of certain environment variables.
// needsDotSlash indicates that s is something like a command
// and must contain at least one path separator (because "." is
// by sensible default not on paths).
func trimCwd(s, cwd string, needsDotSlash bool) string {
	if s == cwd {
		return "."
	}
	if strings.HasPrefix(s, cwd+"/") {
		s = s[1+len(cwd):]
	} else if strings.HasPrefix(s, cwd+string(filepath.Separator)) {
		s = s[len(cwd+string(filepath.Separator)):]
	} else {
		return shorten(s)
	}
	if needsDotSlash {
		s = "." + string(filepath.Separator) + s
	}
	return s
}

// shorten replaces instances of $EV in a string.
// EV is one of PWD, GOROOT, GOPATH, and HOME.
func shorten(s string) string {
	if shortenEVs == "" {
		return s
	}
	for _, a := range abbreviations {
		s = strings.ReplaceAll(s, a.substring, a.replace)
	}
	return s
}
