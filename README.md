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

Finally, run 
```
gclsp_prof $PWD/somedir myapp.prof
```

This will produce output that looks something like:
```
~/work/bent$ gclsp_prof ./gclsp /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/gonum_community_Go_0.prof
  4.5%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:390 :: cannotInlineFunction, marked go:cgo_unsafe_args (at nearby line 389)
  4.5%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:390 :: nilcheck (at line 390)
  3.6%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:201 :: cannotInlineFunction, marked go:cgo_unsafe_args (at nearby line 200)
  3.6%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:201 :: nilcheck (at line 201)
  2.7%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:867 :: isInBounds (at line 867)
  2.7%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:867 :: isInBounds (at nearby line 868)
  2.7%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:404 :: cannotInlineFunction, marked go:cgo_unsafe_args (at nearby line 403)
  2.7%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:404 :: nilcheck (at line 404)
  2.7%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:168 :: cannotInlineFunction, marked go:cgo_unsafe_args (at nearby line 167)
  2.7%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:168 :: nilcheck (at line 168)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:875 :: isInBounds (at line 875)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:875 :: isInBounds (at line 875)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:599 :: isInBounds (at line 599)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:599 :: isInBounds (at line 599)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:601 :: isInBounds (at nearby line 599)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:601 :: isInBounds (at nearby line 599)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_directed_multiplex.go:601 :: isInBounds (at line 601)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_common.go:367 :: cannotInlineFunction, function too complex: cost 83 exceeds budget 80 (at nearby line 366)
  1.8%, /Users/drchase/work/bent/gopath/src/gonum.org/v1/gonum/graph/community/louvain_common.go:367 :: escape (at nearby line 366)
  1.8%, /Users/drchase/work/bent/goroots/Go/src/runtime/malloc.go:891 :: cannotInlineFunction, unhandled op CLOSURE (at nearby line 890)
  1.8%, /Users/drchase/work/bent/goroots/Go/src/runtime/mprof.go:229 :: isInBounds (at line 229)
  1.8%, /Users/drchase/work/bent/goroots/Go/src/runtime/mprof.go:229 :: nilcheck (at line 229)
  1.8%, /Users/drchase/work/bent/goroots/Go/src/runtime/mprof.go:229 :: isSliceInBounds (at nearby line 230)
  1.8%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:239 :: cannotInlineFunction, marked go:cgo_unsafe_args (at nearby line 238)
  1.8%, /Users/drchase/work/bent/goroots/Go/src/runtime/sys_darwin.go:239 :: nilcheck (at line 239)
```

