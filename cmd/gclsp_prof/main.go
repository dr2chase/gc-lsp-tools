// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strings"

	"github.com/dr2chase/gc-lsp-tools/lsp"
	"github.com/dr2chase/gc-lsp-tools/prof"

	"github.com/rdleal/intervalst/interval"
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
var packages string

var verbose = false
var before = int64(0)
var after = int64(0)
var explain = false
var cpuprofile = ""
var threshold = 1.0
var filter = ""
var filterRE *regexp.Regexp

// gclsp_prof [-v] [-e] [-a=n] [-b=n] [-f=RE] [-t=f.f] [-s=EVs] [-cpuprofile=file]  lspdir profile1 [ profile2 ... ]
// Produces a summary of optimizations (if any) that were not or could not be applied at hotspots in the profile.
func main() {

	flag.BoolVar(&verbose, "v", verbose, "Spews information about profiles read and lsp files")
	flag.BoolVar(&explain, "e", explain, "Also supply \"explanations\"")
	flag.Int64Var(&after, "a", after, "Include log entries this many lines after a profile hot spot")
	flag.Int64Var(&before, "b", before, "Include log entries this many lines before a profile hot spot")

	flag.StringVar(&filter, "f", filter, "Reported tags should match filter")
	flag.Float64Var(&threshold, "t", threshold, "Threshold percentage below which profile entries will be ignored")
	flag.StringVar(&shortenEVs, "s", shortenEVs, "Environment variables used to abbreviate file names in output")

	flag.StringVar(&cpuprofile, "cpuprofile", cpuprofile, "Record a cpu profile in this file")
	flag.StringVar(&bench, "bench", bench, "Run 'bench' benchmarks in current directory and reports hotspot(s). Passes -bench=whatever to go test, as well as arguments past --")
	flag.StringVar(&keep, "keep", keep, "For -bench, keep the intermedia results in <-keep>.lspdir and <-keep>.prof")
	flag.StringVar(&packages, "packages", packages, "For -bench, get diagnostics for the listed packages (see 'go help packages')")

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

	if filter != "" {
		filterRE = regexp.MustCompile(filter)
	}

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
	pi, err := prof.FromProtoBuf(profiles, true)

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
	err = lsp.ReadAll(lspDir, byFile, verbose)
	if err != nil {
		panic(err)
	}

	reportPlain(pi, byFile)

}

func reportPlain(pi []*prof.ProfileItem, byFile map[string]*lsp.CompilerDiagnostics) {
	near := func(d *lsp.Diagnostic, line int64) bool {
		diagStart := int64(d.Range.Start.Line)
		diagEnd := int64(d.Range.End.Line)
		return line-before <= diagStart && diagEnd <= line+after
	}

	tab := "        " // Tabs vary, we want 8.

	for _, p := range pi {
		if p.FlatPercent >= threshold {
			cd := byFile[p.FileLine[0].SourceFile]
			if cd != nil && len(cd.Diagnostics) > 0 {
				printedProfileLine := false
				profileInlines := p.FileLine[1:]
				for i, fl := range p.FileLine {
					p.FileLine[i].SourceFile = shorten(fl.SourceFile)
				}
				fl := p.FileLine[0]

				// Defer printing profile line till at least one diagnostic is shown to match
				for _, d := range cd.Diagnostics {
					if d.Code == "inlineCall" { // Don't want to see these, they are confusing and eventually removed..
						continue
					}

					if filterRE != nil && !filterRE.MatchString(d.Code) {
						continue
					}

					if !near(d, fl.Line) {
						continue
					}
					if !printedProfileLine {
						printedProfileLine = true

						fmt.Printf("%5.1f%%, %s:%d)\n", p.FlatPercent, fl.SourceFile, fl.Line)

						for _, il := range profileInlines {
							fmt.Printf("%12s(inline) %s:%d\n", tab, il.SourceFile, il.Line)
						}
					}

					nearby := ""
					if int64(d.Range.End.Line) < p.FileLine[0].Line {
						nearby = "earlier "
					}
					if int64(d.Range.Start.Line) > p.FileLine[0].Line {
						nearby = "later "
					}

					// Now it's known if it's nearby or not, start printing....
					if d.Message != "" { // Note '%5.1f%%, ' is 8 runes wide
						fmt.Printf("%8s%s, %s (at %sline %d)\n", tab, d.Code, d.Message, nearby, d.Range.Start.Line)
					} else {
						fmt.Printf("%8s%s (at %sline %d)\n", tab, d.Code, nearby, d.Range.Start.Line)
					}

					diagnosticInlines, remainingRelated := inlinesFromRelated(d.RelatedInformation)

					// Sort through inlines first to determine if this is an exact match or earlier/later/nearby
					var diagLocs []string
					inlineNearby := ""
					for i, j := 0, 0; i < len(profileInlines) || j < len(diagnosticInlines); i, j = i+1, j+1 {
						// Adjust declarations of "nearness", for inlines insensitive to before/after for now.
						if inlineNearby == "" && nearby == "" {
							if i < len(profileInlines) && j < len(diagnosticInlines) {
								if diagnosticInlines[j].SourceFile != profileInlines[i].SourceFile {
									inlineNearby = "-nearby" // different files
								} else if diagnosticInlines[j].LineStart > profileInlines[i].Line {
									inlineNearby = "-later"
								} else if diagnosticInlines[j].LineEnd < profileInlines[i].Line {
									inlineNearby = "-earlier"
								} else {
									// still the same
								}
							} else {
								inlineNearby = "-nearby" // mismatched depths
							}
						}

						if j < len(diagnosticInlines) {
							il := diagnosticInlines[j]
							if il.LineStart == il.LineEnd {
								diagLocs = append(diagLocs, fmt.Sprintf("(inline%s) %s:%d", inlineNearby, il.SourceFile, il.LineStart))
							} else {
								diagLocs = append(diagLocs, fmt.Sprintf("(inline%s) %s:%d-%d", inlineNearby, il.SourceFile, il.LineStart, il.LineEnd))

							}
							nearby = "not empty" // prevent repeats
							inlineNearby = ""
						} else {
							break // Exit after noticing that the depths are mismatched
						}
					}

					// Print inline information, as necessary
					for i := 0; i < len(diagLocs); i++ {
						fmt.Printf("%16s%s\n", tab, diagLocs[i])
					}

					// Handle extended "explanations".
					if explain {
						// TODO if explanations ever span multiple lines, change this (LineStart -> LineStart...LineEnd)
						for len(remainingRelated) > 0 {
							fl := fileLineFromRelated(&remainingRelated[0])
							fmt.Printf("%12sexplanation :: %s:%d, %s\n", tab, fl.SourceFile, fl.LineStart, remainingRelated[0].Message)
							diagnosticInlines, remainingRelated = inlinesFromRelated(remainingRelated[1:])
							for _, fl := range diagnosticInlines {
								//
								fmt.Printf("%18s(inline) %s:%d\n", tab, fl.SourceFile, fl.LineStart)
							}
						}
					}
				}
			}
		}
	}
}

type taggedDiagnostic struct {
	// note that the source file is implicit in an lsp CompilerDiagnostic
	sourceFile string
	diagnostic *lsp.CompilerDiagnostics
}

type grouped struct {
	FlatPercent float64
	samples     []prof.ProfileItem
	diagnostics []taggedDiagnostic
}

type FileLineRange struct {
	SourceFile         string
	LineStart, LineEnd int64
}

// fileLineFromRelated returns the normalized file and line number from the
// Location of its DiagnosticRelatedInformation argument.
func fileLineFromRelated(ri *lsp.DiagnosticRelatedInformation) FileLineRange {
	uri := string(ri.Location.URI)
	if strings.HasPrefix(uri, "file://") {
		s, err := url.PathUnescape(uri[7:])
		if err != nil { // TODO should we say something?
			s = uri[7:]
		}
		uri = s
	}
	return FileLineRange{
		SourceFile: shorten(uri),
		LineStart:  int64(ri.Location.Range.Start.Line),
		LineEnd:    int64(ri.Location.Range.End.Line),
	}
}

// inlinesFromRelated extracts any inline locations from a slice of DiagnosticRelatedInformation
// and returns the file+line for the inlines and the remaining subslice of DiagnosticRelatedInformation.
func inlinesFromRelated(relatedInformation []lsp.DiagnosticRelatedInformation) ([]FileLineRange, []lsp.DiagnosticRelatedInformation) {
	var diagnosticInlines []FileLineRange
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

	if packages != "" {
		packages = packages + "="
	}
	gcFlags := "-gcflags=" + packages + "-json=0," + lsp

	cmdArgs := []string{"test", gcFlags, "-cpuprofile=" + cpuprofile, "-bench=" + bench, "."}
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
