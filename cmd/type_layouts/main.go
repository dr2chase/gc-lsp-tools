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

	var pi []*prof.ProfileItem
	var err error

	if len(profiles) > 0 {
		// pi, err := prof.FromTextOutput(profiles)
		pi, err = prof.FromProtoBuf(profiles, true, true, int(verbose))
		if err != nil {
			fmt.Fprintf(os.Stderr, "FromProtoBuf error %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "FromProtoBuf returns pi, len=%d\n", len(pi))
		}
	}

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
	typeToInners := make(map[string][]prof.FileLine)
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
				}
				seen[fileLineCode{fl, x.Code}] = unit
				innerToType[fl] = append(innerToType[fl], typeAndWeight{ty, weight})
				typeToInners[ty] = append(typeToInners[ty], fl)
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
		fmt.Fprintf(os.Stderr, "sort, len(types)=%d\n", len(types))
	}
	sort.SliceStable(types, func(i, j int) bool {
		if len(types[i]) == len(types[j]) {
			return types[i] < types[j]
		}
		return len(types[i]) < len(types[j])
	})
	fmt.Printf(
		"type,alloc,percent,file,line," +
			"plain_size,plain_heap,plain_span,pI,pT," +
			"sort_size,sort_heap,sort_span,sI,sT," +
			"sortFill_size,sortFill_heap,sortFill_span,pSFI,pSFT," +
			"gp_size,gp_heap,gp_span,gI,gT," +
			"gpSortFill_size,gpSortFill_heap,gSF_span,gSFI,gSFT\n")

	if verbose > 0 {
		fmt.Fprintf(os.Stderr, "layouts\n")
	}

	var plainTotal, gpTotal, gpsfTotal, sfTotal, sfCompTotal float64

	for _, t := range types {
		if verbose > 1 {
			fmt.Fprintf(os.Stderr, "%s\n", t)
		}

		plainBytes, plainSize, plainAlign, _, plainPtrSpan, _ := layouts.Builtins.Plain(t)
		sortBytes, sortSize, sortAlign, _, sortPtrSpan, _ := layouts.Builtins.Sort(t)
		sfBytes, sfSize, sfAlign, _, sfPtrSpan, _ := layouts.Builtins.SortFill(t)

		gpBytes, gpSize, gpAlign, _, gpPtrSpan, _ := layouts.GpBuiltins.Plain(t)
		gpSfBytes, gpSfSize, gpSfAlign, _, gpSfPtrSpan, _ := layouts.GpBuiltins.SortFill(t)
		// _, gpSortSize, _, _, gpSortSpan, _ := layouts.GpBuiltins.Sort(t)
		// compressedBytes, compressedSize, compressedAlign, _, compressedPtrSpan, _ := layouts.Compressed.Plain(t)
		// _, gpMinusSize, _, _, _, _ := layouts.GpMinusBuiltins.Plain(t)

		// compSfBytes, compressedSortFillSize, compSfAlign, _, compressedSortFillSpan, _ := layouts.Compressed.SortFill(t)

		// _, _, _, _ = compressedSize, compressedPtrSpan, compressedSortFillSize, compressedSortFillSpan

		check(t, plainBytes)
		// check(t, compressedBytes)
		check(t, gpBytes)
		check(t, gpSfBytes)
		// check(t, compSfBytes)

		alloced := typeToAlloc[t]

		if plainSize > 0 {
			sortSize = (sortSize + sortAlign - 1) & -sortAlign
			sfSize = (sfSize + sfAlign - 1) & -sfAlign
			gpSize = (gpSize + gpAlign - 1) & -gpAlign
			gpSfSize = (gpSfSize + gpSfAlign - 1) & -gpSfAlign
			// compressedSortFillSize = (compressedSortFillSize + compSfAlign - 1) & -compSfAlign

			plainTotal += alloced * float64(plainSize) / float64(plainSize)
			gpTotal += alloced * float64(gpSize) / float64(plainSize)
			gpsfTotal += alloced * float64(gpSfSize) / float64(plainSize)
			sfTotal += alloced * float64(sfSize) / float64(plainSize)
			// sfCompTotal += alloced * float64(compressedSortFillSize) / float64(plainSize)
		}
		if alloced/sampleTotal >= threshold/100 || len(pi) == 0 {
			fl := typeToInners[t][0]
			file, line := fl.SourceFile, fl.Line
			//            alloc p   pi    pt   g   gi    gt   g- ss gss    sfpi  sfpt    sfgi  sfgt
			text := fmt.Sprintf("%s,%1.0f, %1.2f%%, %s,%d", t, alloced, 100*alloced/sampleTotal, file, line)
			textFn := func(size, align int, bytes []layouts.Builtin, ptrSpan int) string {
				// 			"plain_size,plain_heap,plain_span,pI,pT," +
				fracI, fracT := quality(layouts.Measure(bytes, align))
				size = (size + align - 1) & -align

				return fmt.Sprintf(", %d,%d,%d,%1.2f,%1.2f",
					size, roundupsize(uintptr(size), ptrSpan == 0), ptrSpan, fracI, fracT)
			}
			fmt.Print(text)
			fmt.Print(textFn(plainSize, plainAlign, plainBytes, plainPtrSpan))
			fmt.Print(textFn(sortSize, sortAlign, sortBytes, sortPtrSpan))
			fmt.Print(textFn(sfSize, sfAlign, sfBytes, sfPtrSpan))
			fmt.Print(textFn(gpSize, gpAlign, gpBytes, gpPtrSpan))
			fmt.Print(textFn(gpSfSize, gpSfAlign, gpSfBytes, gpSfPtrSpan))
			fmt.Println()
			// compressedSize, cI, cT, compressedSortFillSize, sfcI, sfcT)
		}
	}

	if len(pi) > 0 {
		fmt.Fprintf(os.Stderr, "plain total, ratio, %1.0f,%1.3f\n", plainTotal, plainTotal/plainTotal)
		fmt.Fprintf(os.Stderr, "sort+fill total, ratio, %1.0f,%1.3f\n", sfTotal, sfTotal/plainTotal)
		fmt.Fprintf(os.Stderr, "sort+fill+compressed total, ratio, %1.0f,%1.3f\n", sfCompTotal, sfCompTotal/plainTotal)
		fmt.Fprintf(os.Stderr, "gopherPen total, ratio, %1.0f,%1.3f\n", gpTotal, gpTotal/plainTotal)
		fmt.Fprintf(os.Stderr, "gopherPen+sort+fill total, ratio, %1.0f,%1.3f\n", gpsfTotal, gpsfTotal/plainTotal)
	}
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

const (
	maxTinySize   = _TinySize
	tinySizeClass = _TinySizeClass
	maxSmallSize  = _MaxSmallSize

	pageShift = _PageShift
	pageSize  = _PageSize

	_PageSize = 1 << _PageShift
	_PageMask = _PageSize - 1

	// _64bit = 1 on 64-bit systems, 0 on 32-bit systems
	_64bit = 1 << (^uintptr(0) >> 63) / 2

	// Tiny allocator parameters, see "Tiny allocator" comment in malloc.go.
	_TinySize      = 16
	_TinySizeClass = int8(2)

	_FixAllocChunk = 16 << 10 // Chunk size for FixAlloc

	// Per-P, per order stack segment cache size.
	_StackCacheSize = 32 * 1024
)

// class  bytes/obj  bytes/span  objects  tail waste  max waste  min align
//     1          8        8192     1024           0     87.50%          8
//     2         16        8192      512           0     43.75%         16
//     3         24        8192      341           8     29.24%          8
//     4         32        8192      256           0     21.88%         32
//     5         48        8192      170          32     31.52%         16
//     6         64        8192      128           0     23.44%         64
//     7         80        8192      102          32     19.07%         16
//     8         96        8192       85          32     15.95%         32
//     9        112        8192       73          16     13.56%         16
//    10        128        8192       64           0     11.72%        128
//    11        144        8192       56         128     11.82%         16
//    12        160        8192       51          32      9.73%         32
//    13        176        8192       46          96      9.59%         16
//    14        192        8192       42         128      9.25%         64
//    15        208        8192       39          80      8.12%         16
//    16        224        8192       36         128      8.15%         32
//    17        240        8192       34          32      6.62%         16
//    18        256        8192       32           0      5.86%        256
//    19        288        8192       28         128     12.16%         32
//    20        320        8192       25         192     11.80%         64
//    21        352        8192       23          96      9.88%         32
//    22        384        8192       21         128      9.51%        128
//    23        416        8192       19         288     10.71%         32
//    24        448        8192       18         128      8.37%         64
//    25        480        8192       17          32      6.82%         32
//    26        512        8192       16           0      6.05%        512
//    27        576        8192       14         128     12.33%         64
//    28        640        8192       12         512     15.48%        128
//    29        704        8192       11         448     13.93%         64
//    30        768        8192       10         512     13.94%        256
//    31        896        8192        9         128     15.52%        128
//    32       1024        8192        8           0     12.40%       1024
//    33       1152        8192        7         128     12.41%        128
//    34       1280        8192        6         512     15.55%        256
//    35       1408       16384       11         896     14.00%        128
//    36       1536        8192        5         512     14.00%        512
//    37       1792       16384        9         256     15.57%        256
//    38       2048        8192        4           0     12.45%       2048
//    39       2304       16384        7         256     12.46%        256
//    40       2688        8192        3         128     15.59%        128
//    41       3072       24576        8           0     12.47%       1024
//    42       3200       16384        5         384      6.22%        128
//    43       3456       24576        7         384      8.83%        128
//    44       4096        8192        2           0     15.60%       4096
//    45       4864       24576        5         256     16.65%        256
//    46       5376       16384        3         256     10.92%        256
//    47       6144       24576        4           0     12.48%       2048
//    48       6528       32768        5         128      6.23%        128
//    49       6784       40960        6         256      4.36%        128
//    50       6912       49152        7         768      3.37%        256
//    51       8192        8192        1           0     15.61%       8192
//    52       9472       57344        6         512     14.28%        256
//    53       9728       49152        5         512      3.64%        512
//    54      10240       40960        4           0      4.99%       2048
//    55      10880       32768        3         128      6.24%        128
//    56      12288       24576        2           0     11.45%       4096
//    57      13568       40960        3         256      9.99%        256
//    58      14336       57344        4           0      5.35%       2048
//    59      16384       16384        1           0     12.49%       8192
//    60      18432       73728        4           0     11.11%       2048
//    61      19072       57344        3         128      3.57%        128
//    62      20480       40960        2           0      6.87%       4096
//    63      21760       65536        3         256      6.25%        256
//    64      24576       24576        1           0     11.45%       8192
//    65      27264       81920        3         128     10.00%        128
//    66      28672       57344        2           0      4.91%       4096
//    67      32768       32768        1           0     12.50%       8192

// alignment  bits  min obj size
//         8     3             8
//        16     4            32
//        32     5           256
//        64     6           512
//       128     7           768
//      4096    12         28672
//      8192    13         32768

const (
	_MaxSmallSize   = 32768
	smallSizeDiv    = 8
	smallSizeMax    = 1024
	largeSizeDiv    = 128
	_NumSizeClasses = 68
	_PageShift      = 13
	maxObjsPerSpan  = 1024

	mallocHeaderSize       = 8
	minSizeForMallocHeader = 512 // for 64 bit
)

var class_to_size = [_NumSizeClasses]uint16{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}
var class_to_allocnpages = [_NumSizeClasses]uint8{0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1, 2, 1, 2, 1, 3, 2, 3, 1, 3, 2, 3, 4, 5, 6, 1, 7, 6, 5, 4, 3, 5, 7, 2, 9, 7, 5, 8, 3, 10, 7, 4}
var class_to_divmagic = [_NumSizeClasses]uint32{0, ^uint32(0)/8 + 1, ^uint32(0)/16 + 1, ^uint32(0)/24 + 1, ^uint32(0)/32 + 1, ^uint32(0)/48 + 1, ^uint32(0)/64 + 1, ^uint32(0)/80 + 1, ^uint32(0)/96 + 1, ^uint32(0)/112 + 1, ^uint32(0)/128 + 1, ^uint32(0)/144 + 1, ^uint32(0)/160 + 1, ^uint32(0)/176 + 1, ^uint32(0)/192 + 1, ^uint32(0)/208 + 1, ^uint32(0)/224 + 1, ^uint32(0)/240 + 1, ^uint32(0)/256 + 1, ^uint32(0)/288 + 1, ^uint32(0)/320 + 1, ^uint32(0)/352 + 1, ^uint32(0)/384 + 1, ^uint32(0)/416 + 1, ^uint32(0)/448 + 1, ^uint32(0)/480 + 1, ^uint32(0)/512 + 1, ^uint32(0)/576 + 1, ^uint32(0)/640 + 1, ^uint32(0)/704 + 1, ^uint32(0)/768 + 1, ^uint32(0)/896 + 1, ^uint32(0)/1024 + 1, ^uint32(0)/1152 + 1, ^uint32(0)/1280 + 1, ^uint32(0)/1408 + 1, ^uint32(0)/1536 + 1, ^uint32(0)/1792 + 1, ^uint32(0)/2048 + 1, ^uint32(0)/2304 + 1, ^uint32(0)/2688 + 1, ^uint32(0)/3072 + 1, ^uint32(0)/3200 + 1, ^uint32(0)/3456 + 1, ^uint32(0)/4096 + 1, ^uint32(0)/4864 + 1, ^uint32(0)/5376 + 1, ^uint32(0)/6144 + 1, ^uint32(0)/6528 + 1, ^uint32(0)/6784 + 1, ^uint32(0)/6912 + 1, ^uint32(0)/8192 + 1, ^uint32(0)/9472 + 1, ^uint32(0)/9728 + 1, ^uint32(0)/10240 + 1, ^uint32(0)/10880 + 1, ^uint32(0)/12288 + 1, ^uint32(0)/13568 + 1, ^uint32(0)/14336 + 1, ^uint32(0)/16384 + 1, ^uint32(0)/18432 + 1, ^uint32(0)/19072 + 1, ^uint32(0)/20480 + 1, ^uint32(0)/21760 + 1, ^uint32(0)/24576 + 1, ^uint32(0)/27264 + 1, ^uint32(0)/28672 + 1, ^uint32(0)/32768 + 1}
var size_to_class8 = [smallSizeMax/smallSizeDiv + 1]uint8{0, 1, 2, 3, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16, 17, 17, 18, 18, 19, 19, 19, 19, 20, 20, 20, 20, 21, 21, 21, 21, 22, 22, 22, 22, 23, 23, 23, 23, 24, 24, 24, 24, 25, 25, 25, 25, 26, 26, 26, 26, 27, 27, 27, 27, 27, 27, 27, 27, 28, 28, 28, 28, 28, 28, 28, 28, 29, 29, 29, 29, 29, 29, 29, 29, 30, 30, 30, 30, 30, 30, 30, 30, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32, 32}
var size_to_class128 = [(_MaxSmallSize-smallSizeMax)/largeSizeDiv + 1]uint8{32, 33, 34, 35, 36, 37, 37, 38, 38, 39, 39, 40, 40, 40, 41, 41, 41, 42, 43, 43, 44, 44, 44, 44, 44, 45, 45, 45, 45, 45, 45, 46, 46, 46, 46, 47, 47, 47, 47, 47, 47, 48, 48, 48, 49, 49, 50, 51, 51, 51, 51, 51, 51, 51, 51, 51, 51, 52, 52, 52, 52, 52, 52, 52, 52, 52, 52, 53, 53, 54, 54, 54, 54, 55, 55, 55, 55, 55, 56, 56, 56, 56, 56, 56, 56, 56, 56, 56, 56, 57, 57, 57, 57, 57, 57, 57, 57, 57, 57, 58, 58, 58, 58, 58, 58, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 61, 61, 61, 61, 61, 62, 62, 62, 62, 62, 62, 62, 62, 62, 62, 62, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67}

// divRoundUp returns ceil(n / a).
func divRoundUp(n, a uintptr) uintptr {
	// a is generally a power of two. This will get inlined and
	// the compiler will optimize the division.
	return (n + a - 1) / a
}

func roundupsize(size uintptr, noscan bool) (reqSize uintptr) {
	reqSize = size
	if reqSize <= maxSmallSize-mallocHeaderSize {
		// Small object.
		if !noscan && reqSize > minSizeForMallocHeader { // !noscan && !heapBitsInSpan(reqSize)
			reqSize += mallocHeaderSize
		}
		// (reqSize - size) is either mallocHeaderSize or 0. We need to subtract mallocHeaderSize
		// from the result if we have one, since mallocgc will add it back in.
		if reqSize <= smallSizeMax-8 {
			return uintptr(class_to_size[size_to_class8[divRoundUp(reqSize, smallSizeDiv)]])
		}
		return uintptr(class_to_size[size_to_class128[divRoundUp(reqSize-smallSizeMax, largeSizeDiv)]])
	}
	// Large object. Align reqSize up to the next page. Check for overflow.
	reqSize += pageSize - 1
	if reqSize < size {
		return size
	}
	return reqSize &^ (pageSize - 1)
}
