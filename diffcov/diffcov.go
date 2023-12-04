// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diffcov

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
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

func DoDiffs(diffs string, coverprofile string, diffDir string, verbose reuse.Count) {
	byt, err := ioutil.ReadFile(diffs)
	must(err)
	diff, err := diffparser.Parse(string(byt))
	must(err)
	var coverage map[string][]coverLine
	if coverprofile != "" {
		coverage = readCoverProfile(coverprofile)
	}
	for _, f := range diff.Files {
		fn := f.NewName
		if diffDir != "" {
			fn = filepath.Join(diffDir, fn)
		}
		lines := ReadFile(fn)
		var covered []coverLine
		if coverage != nil {
			for k, v := range coverage {
				if strings.HasSuffix(k, f.NewName) {
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
				if verbose > 1 {
					fmt.Printf("%s: %s\t%5d %s %T %s\n", status, f.NewName, l.Number, mode, lines[l.Number], l.Content)
				} else if status == "Untested" || verbose > 0 {
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

	buf, err := ioutil.ReadFile(fName)
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

// ReadFile parse fileName and returns a map of lines to statements.
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