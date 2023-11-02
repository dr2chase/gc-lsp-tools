// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//  head -1 logopt/%00/x.json
//  {"version":0,"package":"\u0000","goos":"darwin","goarch":"amd64","gc_version":"devel +86487adf6a Thu Nov 7 19:34:56 2019 -0500","file":"x.go"}

type VersionHeader struct {
	Version   int    `json:"version"`
	Package   string `json:"package"`
	Goos      string `json:"goos"`
	Goarch    string `json:"goarch"`
	GcVersion string `json:"gc_version"`
	File      string `json:"file,omitempty"` // LSP requires an enclosing resource, i.e., a file
}

// DocumentURI, Position, Range, Location, Diagnostic, DiagnosticRelatedInformation all reuse json definitions from gopls.
// See https://github.com/golang/tools/blob/22afafe3322a860fcd3d88448768f9db36f8bc5f/internal/lsp/protocol/tsprotocol.go

type DocumentURI string

type Position struct {
	Line      uint `json:"line"`      // gopls uses float64, but json output is the same for integers
	Character uint `json:"character"` // gopls uses float64, but json output is the same for integers
}

// A Range in a text document expressed as (zero-based) start and end positions.
// A range is comparable to a selection in an editor. Therefore the end position is exclusive.
// If you want to specify a range that contains a line including the line ending character(s)
// then use an end position denoting the start of the next line.
type Range struct {
	/*Start defined:
	 * The range's start position
	 */
	Start Position `json:"start"`

	/*End defined:
	 * The range's end position
	 */
	End Position `json:"end"` // exclusive
}

// A Location represents a location inside a resource, such as a line inside a text file.
type Location struct {
	// URI is
	URI DocumentURI `json:"uri"`

	// Range is
	Range Range `json:"range"`
}

/* DiagnosticRelatedInformation defined:
 * Represents a related message and source code location for a diagnostic. This should be
 * used to point to code locations that cause or related to a diagnostics, e.g when duplicating
 * a symbol in a scope.
 */
type DiagnosticRelatedInformation struct {

	/*Location defined:
	 * The location of this related diagnostic information.
	 */
	Location Location `json:"location"`

	/*Message defined:
	 * The message of this related diagnostic information.
	 */
	Message string `json:"message"`
}

// DiagnosticSeverity defines constants
type DiagnosticSeverity uint

const (
	/*SeverityInformation defined:
	 * Reports an information.
	 */
	SeverityInformation DiagnosticSeverity = 3
)

// DiagnosticTag defines constants
type DiagnosticTag uint

/*Diagnostic defined:
 * Represents a diagnostic, such as a compiler error or warning. Diagnostic objects
 * are only valid in the scope of a resource.
 */
type Diagnostic struct {

	/*Range defined:
	 * The range at which the message applies
	 */
	Range Range `json:"range"`

	/*Severity defined:
	 * The diagnostic's severity. Can be omitted. If omitted it is up to the
	 * client to interpret diagnostics as error, warning, info or hint.
	 */
	Severity DiagnosticSeverity `json:"severity,omitempty"` // always SeverityInformation for optimizer logging.

	/*Code defined:
	 * The diagnostic's code, which usually appear in the user interface.
	 */
	Code string `json:"code,omitempty"` // LSP uses 'number | string' = gopls interface{}, but only string here, e.g. "boundsCheck", "nilcheck", etc.

	/*Source defined:
	 * A human-readable string describing the source of this
	 * diagnostic, e.g. 'typescript' or 'super lint'. It usually
	 * appears in the user interface.
	 */
	Source string `json:"source,omitempty"` // "go compiler"

	/*Message defined:
	 * The diagnostic's message. It usually appears in the user interface
	 */
	Message string `json:"message"` // sometimes used, provides additional information.

	/*Tags defined:
	 * Additional metadata about the diagnostic.
	 */
	Tags []DiagnosticTag `json:"tags,omitempty"` // always empty for logging optimizations.

	/*RelatedInformation defined:
	 * An array of related diagnostic information, e.g. when symbol-names within
	 * a scope collide all definitions can be marked via this property.
	 */
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

type CompilerDiagnostics struct {
	Header      *VersionHeader
	Diagnostics []*Diagnostic
}

// ReadFile converts the json-encoded contents of a file (reader)
// into a version header and diagnostics.
func ReadFile(r io.Reader, verbose int) (cd *CompilerDiagnostics, err error) {
	dec := json.NewDecoder(r)
	vh := new(VersionHeader)
	err = dec.Decode(vh)
	if err != nil {
		return
	}
	if verbose > 1 {
		fmt.Fprintf(os.Stderr, "\t\tSource file %s\n", vh.File)
	}
	cd = &CompilerDiagnostics{Header: vh}
	d := new(Diagnostic)
	for err = dec.Decode(d); err == nil; err = dec.Decode(d) {
		cd.Diagnostics = append(cd.Diagnostics, d)
		if verbose > 2 {
			fmt.Fprintf(os.Stderr, "\t\t\t%s:%d %s\n", vh.File, d.Range.Start.Line, d.Code)
		}
		d = new(Diagnostic)
	}
	if err == io.EOF {
		err = nil
	}
	return
}

// ReadPackage opens a directory presumably filled with XXX.json files that
// corresponds to a compiler/optimization information for a single package,
// and converts them to their various CompilerDiagnostics.  The contents
// are self-identifying.
func ReadPackage(dir string, verbose int) (cds []*CompilerDiagnostics, err error) {
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return err
		}
		if strings.HasSuffix(path, ".json") {
			var f *os.File
			f, err = os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			var cd *CompilerDiagnostics
			if verbose > 1 {
				fmt.Fprintf(os.Stderr, "\tReading file %s\n", path)
			}
			cd, err = ReadFile(f, verbose)
			if err != nil {
				return err
			}
			cds = append(cds, cd)
		}
		return err
	})
	return
}

// ReadAll opens a directory of directories, where each directory corresponds to
// a package, and populates a map from (outermost) source file to compiler diagnostics
// for that file.
// Indexing is by outermost file for a diagnostic's position.
func ReadAll(dir string, byFile map[string]*CompilerDiagnostics, verbose int) error {
	first := true
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return err
		}
		if first { // skip self
			first = false
			return err
		}
		if verbose > 0 {
			fmt.Fprintf(os.Stderr, "Reading package directory %s\n", path)
		}
		cds, err := ReadPackage(path, verbose)
		for _, cd := range cds {
			if old, ok := byFile[cd.Header.File]; ok {
				old.Diagnostics = append(old.Diagnostics, cd.Diagnostics...)
				if verbose > 1 {
					fmt.Fprintf(os.Stderr, "Appending %s from %s to data for %s\n", cd.Header.File, cd.Header.Package, old.Header.Package)
				}
			} else {
				byFile[cd.Header.File] = cd
			}
		}
		return filepath.SkipDir
	})
	return err
}
