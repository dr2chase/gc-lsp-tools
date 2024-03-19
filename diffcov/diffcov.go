// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diffcov

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dr2chase/gc-lsp-tools/reuse"
	"github.com/waigani/diffparser"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustParse(s string) int {
	i, err := strconv.ParseInt(s, 10, 64)
	must(err)
	return int(i)
}

type coverLine struct {
	bLine      int
	eLine      int
	stmtCount  int
	coverCount int
}

// type T struct{ A, B int }

// func F(a, b int) *T {
// 	return (*T)(reuse.F(a, b))
// }

func DoDiffs(diffBytes []byte, coverprofile string, diffDir, modDir string, strip int, verbose reuse.Count, showTested bool) {
	diff, err := diffparser.Parse(string(diffBytes))
	must(err)
	var coverage map[string][]coverLine
	var gomodpkg string

	if coverprofile != "" {
		coverage = readCoverProfile(coverprofile)

		// The file names in the coverprofile use the package in go.mod
		gomod := filepath.Join(modDir, "go.mod")
		buf, err := os.ReadFile(gomod)
		if modDir != "" {
			must(err)
		} else {
			modDepth := 0
			// Try to automate go.mod location
			for err != nil && modDepth < 20 {
				modDepth++
				modDir = filepath.Join("..", modDir)
				gomod = filepath.Join(modDir, "go.mod")
				buf, err = os.ReadFile(gomod)
			}
			if verbose > 1 {
				fmt.Fprintf(os.Stderr, "Automated go.mod location returns modDir=%s, modDepth=%d, err=%v\n", modDir, modDepth, err)
			}
			must(err)
		}

		lines := strings.Split(string(buf), "\n")

		for _, s := range lines {
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "module ") {
				gomodpkg = strings.TrimSpace(s[len("module"):])
				break
			}
		}

	}

	for _, f := range diff.Files {
		fn := f.NewName

		if diffDir != "" {
			fn = filepath.Join(diffDir, fn)
		} else {
			// try to find it.
			diffDir = modDir
			computedStrip := 0
			for computedStrip < 20 {
				tfn := filepath.Join(diffDir, fn)
				_, err = os.Stat(tfn)
				if err == nil {
					fn = tfn
					if strip == 0 {
						strip = computedStrip
					}
					if verbose > 1 {
						fmt.Fprintf(os.Stderr, "Automated diffDir location returns diffDir=%s, computedStrip=%d, strip=%d\n",
							diffDir, computedStrip, strip)
					}
					break
				}
				diffDir = filepath.Join(diffDir, "..")
				computedStrip++
			}
			must(err)
		}

		lines := ReadFile(fn)

		pfn := f.NewName
		for i := 0; i < strip; i++ {
			slash := strings.IndexByte(pfn, byte(os.PathSeparator))
			if slash == -1 {
				break
			}
			pfn = pfn[slash+1:]
		}

		var covered []coverLine
		if coverage != nil {

			for k, v := range coverage {
				if verbose > 3 {
					fmt.Fprintf(os.Stderr, "Trying to match %s against %s, gomodpkg=%s\n", pfn, k, gomodpkg)
				}
				if strings.HasSuffix(k, filepath.Join(gomodpkg, pfn)) {
					covered = v
					break
				}
			}
		}
		for _, h := range f.Hunks {
			for _, l := range h.NewRange.Lines {
				mode := []string{"+", "-", " "}[l.Mode]
				if mode == " " {
					continue
				}
				if lines[l.Number] == nil {
					continue
				}
				status := "Untested"
				for _, cl := range covered {
					if cl.coverCount == 0 {
						continue
					}
					if cl.bLine <= l.Number && l.Number <= cl.eLine {
						status = "        "
						break
					}

				}
				if verbose > 2 {
					fmt.Printf("%s: %s\t%5d %s %T %s\n", status, f.NewName, l.Number, mode, lines[l.Number], l.Content)
				} else if status == "Untested" || showTested {
					fmt.Printf("%s: %s\t%5d %s %s\n", status, f.NewName, l.Number, mode, l.Content)
				}
			}
		}
	}
}

var coverRE = regexp.MustCompile("^(.*):([0-9]+)[.]([0-9]+),([0-9]+)[.]([0-9]+) ([0-9]+) ([0-9]+)$")

// readCoverProfile reads the output of "go test -coverprofile XXX"
// and converts it into a map from package-qualified file nams to coverage information.
func readCoverProfile(fName string) map[string][]coverLine {

	coverLines := make(map[string][]coverLine)

	buf, err := os.ReadFile(fName)
	must(err)
	lines := strings.Split(string(buf), "\n")
	// github.com/dr2chase/gc-lsp-tools/layouts/layouts.go:590.21,592.6 1 1
	// 1                                                   2   3  4   5 6 7
	for i, l := range lines {
		if len(l) == 0 {
			continue
		}
		if strings.HasPrefix(l, "mode: ") {
			continue
		}
		parts := coverRE.FindStringSubmatch(l)
		if len(parts) != 8 {
			fmt.Fprintf(os.Stderr, "Line %d failed to match cover line RE, line = '%s'\n", i, l)
			continue
		}

		f := parts[1]
		begin := mustParse(parts[2])
		end := mustParse(parts[4])
		stmtCount := mustParse(parts[6])
		coverCount := mustParse(parts[7])
		coverLines[f] = append(coverLines[f], coverLine{begin, end, stmtCount, coverCount})

	}
	return coverLines

}

// ReadFile parses fileName and returns a map of lines to statements.
func ReadFile(fileName string) (lines map[int]ast.Stmt) {
	lines = make(map[int]ast.Stmt)
	if !strings.HasSuffix(fileName, ".go") {
		return
	}
	fset := token.NewFileSet() // positions are relative to fset

	f, err := parser.ParseFile(fset, fileName, nil, parser.ParseComments)
	if err != nil {
		fmt.Println(err)
		return
	}
	var m map[string]*ast.File
	m = make(map[string]*ast.File)
	m[fileName] = f
	pack, err := ast.NewPackage(fset, m, nil, nil)
	if err != nil { // there are errors because of unresolved packages, ignore those
		// fmt.Println(err)
	}

	myFunc := func(n ast.Node) bool {
		if n == nil {
			return true
		}
		switch n := n.(type) {
		case *ast.Comment:
			return false
		case *ast.CommentGroup:
			return false
			// Crudely, we only care about statements.
		case *ast.EmptyStmt:
			return false
		case ast.Stmt:
			pos := fset.Position(n.Pos())
			if lines[pos.Line] == nil {
				lines[pos.Line] = n
			}
		}
		return true
	}

	ast.Inspect(pack, myFunc)
	return
}
