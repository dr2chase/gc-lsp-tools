# gc-lsp-tools

gclsp_prof reads the Go compiler's LSP-encoded logging of places where an optimization was not quite possible, and combines that with profiling data to see if you might be able to improve performance by tweaking your code slightly.  The use of profiling data is to avoid overwhelming users with noise about all the not-performance-critical places that the optimizer left a check in or was forced to do a heap allocation, etc.

To install:
```
go get -u github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof
```

To use:

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
go build -gcflags=-json=0,$PWD/somedir myapp.go
```
or (all of your application, likely to pick up some GC and runtime code too)
```
go build -gcflags=all=-json=0,$PWD/somedir myapp.go
```
The `-json` option normally wants an absolute path, and the zero is a version number.
[The option logging source code](https://go.googlesource.com/go/+/refs/heads/master/src/cmd/compile/internal/logopt/log_opts.go#24)
explains the flags, directory structure and format for data stored in `$PWD/somedir`.
Version zero is subject to change until the first consumer declares that they need stability,
till then features (escape analysis explanations) and revisions (rationalizing tag names,
new optimizations logged) may appear from time to time.

Then run your application, which will create a profile.

Finally, run, for example
```
.../gclsp_prof/testdata$ gclsp_prof -s -t 5.0 gclsp foo.prof
```

This will produce output that looks something like:
```
gclsp_prof -s -t 5.0 gclsp foo.prof
  5.4%, $PWD/foo.go:93 :: isInBounds (at line 94)
	 :: $PWD/foo.go:20
  7.8%, $PWD/foo.go:94 :: isInBounds (at line 94)
	$PWD/foo.go:20 :: $PWD/foo.go:20
 14.9%, $PWD/foo.go:38 :: isInBounds (at later line 37)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 14.9%, $PWD/foo.go:38 :: isInBounds (at line 38)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 14.9%, $PWD/foo.go:38 :: isInBounds (at line 39)
	$PWD/foo.go:20 :: $PWD/foo.go:20
 16.3%, $PWD/foo.go:38 :: isInBounds (at later line 37)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 16.3%, $PWD/foo.go:38 :: isInBounds (at line 38)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 16.3%, $PWD/foo.go:38 :: isInBounds (at line 39)
	$PWD/foo.go:20 :: $PWD/foo.go:20
```
Inline locations in the list above appear on lines following a sample line,
arranged "sample_line :: diagnostic_line".
They don't always match, especially for "nearby" lines.

To generate the sample data above and also see the commands involved, in
`/github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof`,
run
`go test -v . -here`
which should produce `gclsp` and `foo.prof` for `testdata/foo.go`.

Possibly useful options include:

- -a=n, mention compiler diagnostics from n lines after a hot spot (default 1).
- -b=n, mention compiler diagnostics from n lines before a hot spot (default 2).
- -t=n.f, (a float) samples less hot than the threshold percentage are ignored (default 1.0).
- -s, shorten file names with prefixes that match environment variables PWD, GOROOT, GOPATH, HOME.
- -v, verbose.  You don't want verbose.
- -cpuprofile=file, because every application should have this option.