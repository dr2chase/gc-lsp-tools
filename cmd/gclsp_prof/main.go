package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"github.com/dr2chase/gc-lsp-tools/gclsp"
	"io"
	"os"
	"os/exec"
	"runtime/pprof"
	"strconv"
	"strings"
	"unicode"
)

type profileItem struct {
	//	flat  flat%   sum%        cum   cum%   package.type.methfunc sourcefile:line
	//	0.03s  2.26% 96.99%      0.03s  2.26%  runtime.asyncPreempt /Users/drchase/work/go/src/runtime/preempt_amd64.s:7
	flatSeconds       float64
	flatPercent       float64
	sumPercent        float64
	cumulativeSeconds float64
	cumulativePercent float64
	packageDotType    string
	methodOrFunc      string
	sourceFile        string
	line              int
}

func numberStuff(s string) (n float64, stuff string, rest string) {
	s = strings.TrimSpace(s)
	firstNN := -1
	last := len(s)
	for i, r := range s {
		if !unicode.IsDigit(r) && r != '.'  && firstNN == -1 {
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
	case "ks" : return n * 1000
	case "ms" : return n / 1000
	case "cs" : return n / 100
	case "ds" : return n / 10
	case "us", "µs" : return n/1000000
	case "ns" : return n/1000000000
	}
	return n
}

func lineToProfileItem(s string) *profileItem {
	s0 := s
	pi := profileItem{}
	//	flat  flat%   sum%        cum   cum%   package.type.methfunc sourcefile:line
	//	0.03s  2.26% 96.99%      0.03s  2.26%  runtime.asyncPreempt /Users/drchase/work/go/src/runtime/preempt_amd64.s:7
	n, stuff, s := numberStuff(s)
	if s == "" {
		panic("Unexpected line " + s0)
	}
	pi.flatSeconds = scale(n, stuff)

	pi.flatPercent, stuff, s = numberStuff(s)
	if s == "" || stuff != "%" {
		panic("Unexpected line " + s0 + " stuff= " + stuff)
	}

	pi.sumPercent, stuff, s = numberStuff(s)
	if s == "" || stuff != "%" {
		panic("Unexpected line " + s0)
	}

	n, stuff, s = numberStuff(s)
	if s == "" {
		panic("Unexpected line " + s0)
	}
	pi.cumulativeSeconds = scale(n, stuff)

	pi.cumulativePercent, stuff, s = numberStuff(s)
	if s == "" || stuff != "%" {
		panic("Unexpected line " + s0)
	}

	pi.methodOrFunc, s = nextField(s)
	if s == "" {
		panic("Unexpected line " + s0)
	}

	fileLine, s := nextField(s)
	// TODO it might say (inline) which needs to be dealt with anyway.
	//if s != "" {
	//	panic("Unexpected line " + s0)
	//}

	colon := strings.LastIndexByte(fileLine, ':')
	if colon == -1 {
		panic("Unexpected line " + s0)
	}
	pi.sourceFile = fileLine[0:colon]
	line, err := strconv.ParseInt(fileLine[colon+1:], 10, 32)
	if err != nil {
		panic(err)
	}
	pi.line = int(line)
	return &pi
}

// gclsp_prof lspdir profile1 [ profile2 ... ]
//
func main() {
	verbose := false
	before := 2
	after := 1
	cpuprofile := ""
	threshold := 1.0

	flag.BoolVar(&verbose, "v", verbose, "Spews information about profiles read and lsp files")
	flag.IntVar(&before, "b", before, "Include log entries this many lines before a profile hot spot")
	flag.IntVar(&after, "a", after, "Include log entries this many lines after a profile hot spot")
	flag.StringVar(&cpuprofile, "cpuprofile", cpuprofile, "Record a cpu profile in this file")
	flag.Float64Var(&threshold, "t", threshold, "Threshold percentage below which profile entries will be ignored")

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
		} ()
	}

	args := flag.Args()
	if len(args) < 2 {
		usage()
		os.Exit(1)
	}
	lspDir := args[0]
	cmdArgs := []string{"tool", "pprof", "-text", "-lines", "-flat"}
	cmdArgs = append(cmdArgs, args[1:]...)
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
		return
	}
	r := bufio.NewReader(bytes.NewBuffer(out))
	var pi []*profileItem
	skipping := true
	for i := 1; err != io.EOF; i++ {
		var s string
		s, err = r.ReadString('\n')
		if err != nil && err != io.EOF {
			panic(err)
		}
		if skipping {
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "flat") {
				skipping = false
			}
			continue
		}
		if s == "" {
			continue
		}
		pi = append(pi, lineToProfileItem(s))
	}

	if verbose {
		for _, p := range pi {
			if p.flatPercent >= threshold {
				fmt.Printf("%f%%, %s:%d\n", p.flatPercent, p.sourceFile, p.line)
			}
		}
	}

	byFile := make(map[string]*gclsp.CompilerDiagnostics)
	err = gclsp.ReadAll(lspDir, &byFile, verbose)
	if err != nil {
		panic(err)
	}

	near := func(d *gclsp.Diagnostic, line int) bool {
		diag := int(d.Range.Start.Line)
		return line - before <= diag && diag <= line+after
	}

	for _, p := range pi {
		if p.flatPercent >= 1 {
			cd := byFile[p.sourceFile]
			if cd != nil && len(cd.Diagnostics) > 0 {
				for _, d := range cd.Diagnostics {
					if d.Code == "inlineCall" { // Don't want to see this.
						continue
					}
					if near(d, p.line) {
						nearby := "nearby "
						if int(d.Range.Start.Line) == p.line {
							nearby = ""
						}
						if d.Message != "" {
							fmt.Printf("%5.1f%%, %s:%d :: %s, %s (at %sline %d)\n", p.flatPercent, p.sourceFile, p.line, d.Code, d.Message, nearby, d.Range.Start.Line)
						} else {
							fmt.Printf("%5.1f%%, %s:%d :: %s (at %sline %d)\n", p.flatPercent, p.sourceFile, p.line, d.Code, nearby, d.Range.Start.Line)
						}
					}
				}

			}
		}
	}
}
