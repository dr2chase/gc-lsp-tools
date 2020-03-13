# gc-lsp-tools

gclsp_prof reads the Go compiler's LSP-encoded logging of places where an optimization was not quite possible,
and combines that with profiling data to see if you might be able to improve performance by tweaking your code slightly.
The use of profiling data is to avoid overwhelming users with noise about all the not-performance-critical places
that the optimizer left a check in or was forced to do a heap allocation, etc.

To install:
```
go get -u github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof
```

If you have an existing Go benchmark that exercises the code you'd like to measure,
you can generate and display the missed (or not possible) optimizations at hotspots directly.
For example:
```
# Get a benchmark if you don't have one of your own
git clone https://github.com/dr2chase/gc-lsp-tools'
cd gc-lsp-tools/cmd/gclsp_prof/testdata
# 
gclsp_prof -bench=Bench -keep=bar 
```
and you should see
```
( go test -gcflags=all=-json=0,$PWD/bar.lspdir -cpuprofile=$PWD/bar.prof -bench=Bench . )
goos: darwin
goarch: amd64
pkg: github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof/testdata
BenchmarkDo-4   	       4	 289721244 ns/op
PASS
ok  	github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof/testdata	2.617s

  1.4%, $PWD/foo_test.go:89 :: isInBounds (at line 89)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:15
  1.9%, $PWD/foo_test.go:33 :: isInBounds (at line 33)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:11
  1.9%, $PWD/foo_test.go:89 :: isInBounds (at line 89)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:15
  1.9%, $HOME/work/go/src/runtime/sys_darwin.go:239 :: nilcheck (at line 239)
	 :: $HOME/work/go/src/runtime/proc.go:417
  2.4%, $PWD/foo_test.go:33 :: isInBounds (at line 33)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:11
  3.8%, $PWD/foo_test.go:89 :: isInBounds (at line 89)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:15
  8.0%, $PWD/foo_test.go:33 :: isInBounds (at line 33)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:11
 10.4%, $PWD/foo_test.go:33 :: isInBounds (at line 33)
	$PWD/foo_test.go:15 :: $PWD/foo_test.go:11
```
The two files `bar.lspdir` and `bar.prof` can be used as inputs to rerun the command, perhaps with different
parameters, for example `gclsp_prof -t=3.0 -b=1 bar.lspdir bar.prof`

If you would like to do this to an application rather than a benchmark,
there are additional steps.

First, be sure that your application supports profiling (as Go test and benchmarks do).
Example code to do this:
```
        file, _ := os.Create("foo.prof")
        pprof.StartCPUProfile(file)

        // Insert your interesting code here

        pprof.StopCPUProfile()
        file.Close()
```

Second, compile some or all of your Go (1.14) application with LSP logging enabled:
```
go build -gcflags=-json=0,$PWD/foo.lspdir myapp.go
```
or (all of your application, likely to pick up some GC and runtime code too)
```
go build -gcflags=all=-json=0,$PWD/foo.lspdir myapp.go
```
The `-json` option normally wants an absolute path, and the zero is a version number.
[The compilation-logging source code](https://go.googlesource.com/go/+/refs/heads/master/src/cmd/compile/internal/logopt/log_opts.go#24)
explains the flags, directory structure and format for data stored in `$PWD/foo.lspdir` (but you don't need to know this to use this tool).

Then run your application, which will create a profile, for example `foo.prof`.

Finally, run, for example
```
gclsp_prof -t=5.0 $PWD/foo.lspdir foo.prof
```

This will produce output that looks something like:
```
  5.4%, $PWD/foo.go:93 :: isInBounds (at line 94)
	 :: $PWD/foo.go:20
  7.8%, $PWD/foo.go:94 :: isInBounds (at line 94)
	$PWD/foo.go:20 :: $PWD/foo.go:20
 14.9%, $PWD/foo.go:38 :: isInBounds (at earlier line 37)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 14.9%, $PWD/foo.go:38 :: isInBounds (at line 38)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 14.9%, $PWD/foo.go:38 :: isInBounds (at line 39)
	$PWD/foo.go:20 :: $PWD/foo.go:20
 16.3%, $PWD/foo.go:38 :: isInBounds (at earlier line 37)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 16.3%, $PWD/foo.go:38 :: isInBounds (at line 38)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 16.3%, $PWD/foo.go:38 :: isInBounds (at line 39)
	$PWD/foo.go:20 :: $PWD/foo.go:20
```
Inline locations in the list above appear on lines following a sample line,
arranged "sample_line :: diagnostic_line".

To generate the sample data above and also see the commands involved, in
`github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof`,
run
`go test -v . -here`
which should produce `foo.lspdir` and `foo.prof` for `testdata/foo.go`.

Possibly useful options include:

- -a=n, mention compiler diagnostics from n lines after a hot spot (default 0).
- -b=n, mention compiler diagnostics from n lines before a hot spot (default 0).
- -t=n.f, (a float) samples less hot than the threshold percentage are ignored (default 1.0).
- -e, for diagnosticd with extended explanations (escape analysis soon), also show the extended explanations.
- -bench=Bench..., if not empty, run "go test -bench=Bench... ." with the additional flags necessary to generate
  the lsp information and profile, then run gclsp_prof on those with the other flags.
- -keep=\<basename\>, for -bench, put the lsp and profile files in $PWD/\<basename\>.{lspdir,prof}
- -s=ev1,ev2,...,  list of environment variables to use to shorten paths.  Default "PWD,GOROOT,GOPATH,HOME".
- -cpuprofile=file, because every application should have this option.
- -v, verbose.  You don't want verbose.
