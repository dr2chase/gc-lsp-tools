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
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"

	"github.com/dr2chase/gc-lsp-tools/layouts"
	"github.com/dr2chase/gc-lsp-tools/lsp"
	"github.com/dr2chase/gc-lsp-tools/prof"
	// "github.com/rdleal/intervalst/interval"
)

var pwd string = os.Getenv("PWD")
var goroot string = os.Getenv("GOROOT")
var gopath string = os.Getenv("GOPATH")
var home string = os.Getenv("HOME")

type abbreviation struct{ substring, replace string }

var shortenEVs string = "" // "PWD,GOROOT,GOPATH,HOME"
var abbreviations []abbreviation

var bench string
var keep string
var packages string

var verbose count
var before = int64(0)
var after = int64(0)
var explain = false
var cpuprofile = ""
var memprofile = ""
var threshold = 1.0
var filter = ""
var filterRE *regexp.Regexp

// count is a flag.Value that is like a flag.Bool and a flag.Int.
// If used as -name, it increments the count, but -name=x sets the count.
// Used for verbose flag -v.
type count int

func (c *count) String() string {
	return fmt.Sprint(int(*c))
}

func (c *count) Set(s string) error {
	switch s {
	case "true":
		*c++
	case "false":
		*c = 0
	default:
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid count %q", s)
		}
		*c = count(n)
	}
	return nil
}

func (c *count) Get() interface{} {
	return int(*c)
}

func (c *count) IsBoolFlag() bool {
	return true
}

func (c *count) IsCountFlag() bool {
	return true
}

// gclsp_prof [-v] [-e] [-a=n] [-b=n] [-f=RE] [-t=f.f] [-s=EVs] [-cpuprofile=file]  lspdir // profile1 [ profile2 ... ]
// Reads allocation information from lspdir and tries various methods of laying it out.
func main() {

	flag.Var(&verbose, "v", "Spews increasingly more information about processing.")
	flag.BoolVar(&explain, "e", explain, "Also supply \"explanations\"")
	// flag.Int64Var(&after, "a", after, "Include log entries this many lines after a profile hot spot")
	// flag.Int64Var(&before, "b", before, "Include log entries this many lines before a profile hot spot")

	// flag.StringVar(&filter, "f", filter, "Reported tags should match filter")
	flag.Float64Var(&threshold, "t", threshold, "Threshold percentage below which types will be ignored")
	// flag.StringVar(&shortenEVs, "s", shortenEVs, "Environment variables used to abbreviate file names in output")

	flag.StringVar(&cpuprofile, "cpuprofile", cpuprofile, "Record a cpu profile in this file")
	flag.StringVar(&memprofile, "memprofile", memprofile, "Record a mem profile in this file")
	// flag.StringVar(&bench, "bench", bench, "Run 'bench' benchmarks in current directory and reports hotspot(s). Passes -bench=whatever to go test, as well as arguments past --")
	// flag.StringVar(&keep, "keep", keep, "For -bench, keep the intermedia results in <-keep>.lspdir and <-keep>.prof")
	// flag.StringVar(&packages, "packages", packages, "For -bench, get diagnostics for the listed packages (see 'go help packages')")

	usage := func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			`
%s LspDir reads the compiler logging information in LspDir to experiment with type layouts.
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

	if memprofile != "" {
		file, _ := os.Create(memprofile)
		runtime.MemProfileRate = 1
		defer func() {
			runtime.GC()
			pprof.Lookup("heap").WriteTo(file, 0)
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

	if len(args) < 1 {
		usage()
		os.Exit(1)
	}
	lspDir := args[0]
	profiles := args[1:]

	// pi, err := prof.FromTextOutput(profiles)
	pi, err := prof.FromProtoBuf(profiles, true, true, int(verbose))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FromProtoBuf error %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "FromProtoBuf returns pi, len=%d\n", len(pi))
	}
	// if len(pi) == 0 {
	// 	return
	// }

	byFile := make(map[string]*lsp.CompilerDiagnostics)
	err = lsp.ReadAll(lspDir, byFile, int(verbose))
	if err != nil {
		panic(err)
	}

	reportPlain(byFile, pi, int(verbose))

}

type void struct{}

var unit void

type typeAndWeight struct {
	t string
	w float64
}

type fileLineCode struct {
	fl   prof.FileLine
	code string
}

func quality(d, i, t int) (float64, float64) {
	if d > 0 {
		d, i, t := float64(d), float64(i), float64(t)
		return d / (d + i), d / (d + i + t)
	}
	return 1.0, 1.0
}

func check(t string, b []layouts.Builtin) {
	if err := layouts.Validate(b); err != nil {
		fmt.Fprintf(os.Stderr, "Validate failed %v for type %s\n", err, t)
	}
}

func reportPlain(byFile map[string]*lsp.CompilerDiagnostics, pi []*prof.ProfileItem, verbose int) {
	typeSet := make(map[string]void)
	types := []string{}
	if verbose > 0 {
		fmt.Fprintf(os.Stderr, "reportPlain\n")
	}
	innerToType := make(map[prof.FileLine][]typeAndWeight)
	typeToAlloc := make(map[string]float64)
	seen := make(map[fileLineCode]void)

	for filename, d := range byFile {
		for _, x := range d.Diagnostics {
			if strings.HasPrefix(x.Code, "newobject") {
				weight := 1.0
				if x.Code == "newobjectKey" || x.Code == "newobjectValue" { // sync w/ ssagen/ssa.go
					// TODO make exact for map key and data types
					weight = 0.5
				}
				ty := x.Message
				fl := innermostFileLine(filename, int64(x.Range.Start.Line), x.RelatedInformation)
				if _, ok := seen[fileLineCode{fl, x.Code}]; ok {
					continue
				} else {
					seen[fileLineCode{fl, x.Code}] = unit
				}
				innerToType[fl] = append(innerToType[fl], typeAndWeight{ty, weight})
				if _, ok := typeSet[x.Message]; !ok { // list of all types.
					typeSet[ty] = unit
					types = append(types, ty)
				}
				if verbose > 1 {
					fmt.Fprintf(os.Stderr, "%s:%d %d %s %s\n", fl.SourceFile, fl.Line, len(innerToType[fl]), x.Code, ty)
				}
			} else {
				if verbose > 2 {
					fl := innermostFileLine(filename, int64(x.Range.Start.Line), x.RelatedInformation)
					fmt.Fprintf(os.Stderr, "# %s:%d %s\n", fl.SourceFile, fl.Line, x.Code)
				}

			}
		}
	}

	sampleTotal := 0.0
	for _, p := range pi {
		innermost := p.FileLine[len(p.FileLine)-1]
		twSlice := innerToType[innermost]
		sampleTotal += p.FlatTotal
		if len(twSlice) == 0 {
			fmt.Fprintf(os.Stderr, "Missing type for %2.0f-bytes allocated at %s:%d\n", p.FlatTotal, innermost.SourceFile, innermost.Line)
			typeToAlloc["U"] += p.FlatTotal
		} else {
			for _, tw := range twSlice {
				typeToAlloc[tw.t] += p.FlatTotal * tw.w
				if p.FlatPercent >= threshold*tw.w {
					if verbose > 0 {
						fmt.Fprintf(os.Stderr, "%3.2f%%, %s:%d, %2.1f, %s\n", p.FlatPercent, p.FileLine[0].SourceFile, p.FileLine[0].Line, tw.w, tw.t)
					}
				}
			}
		}
	}

	if verbose > 0 {
		fmt.Fprintf(os.Stderr, "sort\n")
	}
	sort.SliceStable(types, func(i, j int) bool {
		if len(types[i]) == len(types[j]) {
			return types[i] < types[j]
		}
		return len(types[i]) < len(types[j])
	})
	fmt.Printf(
		"type,alloc,percent,plain_size,pI,pT,gp_size,gI,gT,gpMinus_size,sort_size,gpSort_size,sortFill_size,pSFI,pSFT,gpSortFill_size,gSFI,gSFT,plain_span,gp_span,sort_span,gpSort_span,sortFill_span,gpSortFill_span,cpSize,cI,cT,cpSortFillSize,cSFI,cSFI\n")

	if verbose > 0 {
		fmt.Fprintf(os.Stderr, "layouts\n")
	}

	var plainTotal, gpTotal, gpsfTotal, sfTotal, sfCompTotal float64

	for _, t := range types {
		if verbose > 1 {
			fmt.Fprintf(os.Stderr, "%s\n", t)
		}

		plainBytes, plainSize, plainAlign, _, plainPtrSpan, _ := layouts.Builtins.Plain(t)

		compressedBytes, compressedSize, compressedAlign, _, compressedPtrSpan, _ := layouts.Compressed.Plain(t)
		gpBytes, gpSize, gpAlign, _, gpPtrSpan, _ := layouts.GpBuiltins.Plain(t)
		_, gpMinusSize, _, _, _, _ := layouts.GpMinusBuiltins.Plain(t)

		_, sortSize, _, _, sortPtrSpan, _ := layouts.Builtins.Sort(t)
		_, gpSortSize, _, _, gpSortSpan, _ := layouts.GpBuiltins.Sort(t)

		sfBytes, sortFillSize, sfAlign, _, sortFillPtrSpan, _ := layouts.Builtins.SortFill(t)
		gpSfBytes, gpSortFillSize, gpSfAlign, _, gpSortFillSpan, _ := layouts.GpBuiltins.SortFill(t)
		compSfBytes, compressedSortFillSize, compSfAlign, _, compressedSortFillSpan, _ := layouts.Compressed.SortFill(t)

		_, _, _, _ = compressedSize, compressedPtrSpan, compressedSortFillSize, compressedSortFillSpan

		check(t, plainBytes)
		check(t, compressedBytes)
		check(t, gpBytes)
		check(t, gpSfBytes)
		check(t, compSfBytes)

		sortFillSize = (sortFillSize + sfAlign - 1) & -sfAlign
		gpSize = (gpSize + gpAlign - 1) & -gpAlign
		gpSortFillSize = (gpSortFillSize + gpSfAlign - 1) & -gpSfAlign
		compressedSortFillSize = (compressedSortFillSize + compSfAlign - 1) & -compSfAlign

		pI, pT := quality(layouts.Measure(plainBytes, plainAlign))
		cI, cT := quality(layouts.Measure(compressedBytes, compressedAlign))
		gI, gT := quality(layouts.Measure(gpBytes, gpAlign))

		sfpI, sfpT := quality(layouts.Measure(sfBytes, sfAlign))
		sfcI, sfcT := quality(layouts.Measure(compSfBytes, compSfAlign))
		sfgI, sfgT := quality(layouts.Measure(gpSfBytes, gpSfAlign))

		alloced := typeToAlloc[t]

		if plainSize > 0 {
			plainTotal += alloced * float64(plainSize) / float64(plainSize)
			gpTotal += alloced * float64(gpSize) / float64(plainSize)
			gpsfTotal += alloced * float64(gpSortFillSize) / float64(plainSize)
			sfTotal += alloced * float64(sortFillSize) / float64(plainSize)
			sfCompTotal += alloced * float64(compressedSortFillSize) / float64(plainSize)
		}
		if alloced/sampleTotal >= threshold/100 {
			//            alloc p   pi    pt   g   gi    gt   g- ss gss    sfpi  sfpt    sfgi  sfgt
			fmt.Printf("%s,%1.0f, %1.2f%%, %d,%1.2f,%1.2f,%d,%1.2f,%1.2f,%d,%d,%d,%d,%1.2f,%1.2f,%d,%1.2f,%1.2f,%d,%d,%d,%d,%d,%d,%d,%1.2f,%1.2f,%d,%1.2f,%1.2f\n", t, alloced, 100*alloced/sampleTotal,
				plainSize, pI, pT, gpSize, gI, gT, gpMinusSize, sortSize, gpSortSize, sortFillSize, sfpI, sfpT, gpSortFillSize, sfgI, sfgT,
				plainPtrSpan, gpPtrSpan, sortPtrSpan, gpSortSpan, sortFillPtrSpan, gpSortFillSpan, compressedSize, cI, cT, compressedSortFillSize, sfcI, sfcT)
		}
	}

	fmt.Fprintf(os.Stderr, "plain total, ratio, %1.0f,%1.3f\n", plainTotal, plainTotal/plainTotal)
	fmt.Fprintf(os.Stderr, "sort+fill total, ratio, %1.0f,%1.3f\n", sfTotal, sfTotal/plainTotal)
	fmt.Fprintf(os.Stderr, "sort+fill+compressed total, ratio, %1.0f,%1.3f\n", sfCompTotal, sfCompTotal/plainTotal)
	fmt.Fprintf(os.Stderr, "gopherPen total, ratio, %1.0f,%1.3f\n", gpTotal, gpTotal/plainTotal)
	fmt.Fprintf(os.Stderr, "gopherPen+sort+fill total, ratio, %1.0f,%1.3f\n", gpsfTotal, gpsfTotal/plainTotal)
}

type taggedDiagnostic struct {
	// note that the source file is implicit in an lsp CompilerDiagnostic
	sourceFile string
	diagnostic *lsp.CompilerDiagnostics
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

func innermostFileLine(outerFile string, outerLine int64, relatedInformation []lsp.DiagnosticRelatedInformation) prof.FileLine {
	if strings.Contains(outerFile, "graph/graph.go") && outerLine == 373 {
		fmt.Fprintf(os.Stderr, "MISSING LINE\n")
		for _, ri := range relatedInformation {
			if ri.Message != "inlineLoc" {
				break
			}
			flr := fileLineFromRelated(&ri)
			fmt.Fprintf(os.Stderr, "%s:%d\n", flr.SourceFile, flr.LineStart)
		}
	}

	for _, ri := range relatedInformation {
		if ri.Message != "inlineLoc" {
			break
		}
		flr := fileLineFromRelated(&ri)
		outerFile, outerLine = flr.SourceFile, flr.LineStart

		if strings.Contains(outerFile, "graph/graph.go") && outerLine == 373 {
			fmt.Fprintf(os.Stderr, "MISSING LINE\n")
			for _, ri := range relatedInformation {
				if ri.Message != "inlineLoc" {
					break
				}
				flr := fileLineFromRelated(&ri)
				fmt.Fprintf(os.Stderr, "%s:%d\n", flr.SourceFile, flr.LineStart)
			}
		}
	}
	return prof.FileLine{SourceFile: outerFile, Line: outerLine}
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
