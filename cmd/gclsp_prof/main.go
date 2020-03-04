package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/dr2chase/gc-lsp-tools/gclsp"
	"io"
	"os"
	"os/exec"
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

//
func main() {
	verbose := false
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s gc-lsp-dir profile1 [profile2 ...]\n", os.Args[0])
		return
	}
	lspDir := os.Args[1]
	cmdArgs := []string{"tool", "pprof", "-text", "-lines", "-flat"}
	cmdArgs = append(cmdArgs, os.Args[2:]...)
	cmd := exec.Command("go", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Failed to run go tool pprof -text -lines -flat %v\n", cmdArgs)
		fmt.Println(out)
		fmt.Println(err)
		return
	}
	r := bufio.NewReader(bytes.NewBuffer(out))
	var pi []*profileItem
	for i := 1; err != io.EOF; i++ {
		var s string
		s, err = r.ReadString('\n')
		if err != nil && err != io.EOF {
			panic(err)
		}
		if i <= 6 {
			continue
		}
		if s == "" {
			continue
		}
		pi = append(pi, lineToProfileItem(s))
	}

	if verbose {
		for _, p := range pi {
			if p.flatPercent >= 1 {
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
		return line - 2 <= diag && diag <= line+1
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
