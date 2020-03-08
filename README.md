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
        file, _ := os.Create("myapp.prof")
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

Then run your application, which will create a profile.

Finally, run, for example
```
.../gclsp_prof/testdata$ gclsp_prof -s -t 5.0 gclsp foo.prof
```

This will produce output that looks something like:
```
gclsp_prof -s -t 5.0 gclsp foo.prof
  5.6%, $PWD/foo.go:93 :: isInBounds (at nearby line 94)
	 :: $PWD/foo.go:20
  8.6%, $PWD/foo.go:94 :: isInBounds (at line 94)
	$PWD/foo.go:20 :: $PWD/foo.go:20
  9.2%, $PWD/foo.go:94 :: isInBounds (at line 94)
	$PWD/foo.go:20 :: $PWD/foo.go:20
 13.8%, $PWD/foo.go:38 :: isInBounds (at nearby line 37)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 13.8%, $PWD/foo.go:38 :: isInBounds (at line 38)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 13.8%, $PWD/foo.go:38 :: isInBounds (at nearby line 39)
	$PWD/foo.go:20 :: $PWD/foo.go:20
 19.4%, $PWD/foo.go:38 :: isInBounds (at nearby line 37)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 19.4%, $PWD/foo.go:38 :: isInBounds (at line 38)
	$PWD/foo.go:20 :: $PWD/foo.go:16
 19.4%, $PWD/foo.go:38 :: isInBounds (at nearby line 39)
	$PWD/foo.go:20 :: $PWD/foo.go:20
```
Inline locations in the list above appear on lines following a sample line,
arranged "sample_line :: diagnostic_line".
They don't always match, especially for "nearby" lines.

To generate the sample data above, in
`/github.com/dr2chase/gc-lsp-tools/cmd/gclsp_prof`,
run
`go test . -here`
which should produce `gclsp` and `foo.prof` for `testdata/foo.go`.

Possibly useful options include:

- -a=n, mention compiler diagnostics from n lines after a hot spot (default 0).
- -b=n, mention compiler diagnostics from n lines before a hot spot (default 0).
- -t=n.f, (a float) samples less hot than the threshold percentage are ignored (default 1.0).
- -s, shorten file names with prefixes that match environment variables PWD, GOROOT, GOPATH, HOME.
- -v, verbose.  You don't want verbose.
- -cpuprofile=file, because every application should have this option.