// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"runtime/pprof"
)

type Row []float64
type SqMat []Row

func (a SqMat) get(i,j int) float64 {
	return a[i][j]
}

func (a SqMat) put(i,j int, x float64) {
	 a[i][j] = x
}

//go:noinline -- Redundant, but future-proofing against inline of FOR loops
func matrix(n int) SqMat {
	m := make(SqMat, n, n)
	for i := range m {
		m[i] = make(Row, n, n)
	}
	return m
}

//go:noinline -- Redundant, but future-proofing against inline of FOR loops
func transpose(a SqMat) {
	n := len(a) // will assume square and fully populated
	for i := 0; i < n; i++ {
		for j := 0; j < i; j++ {
			t := a.get(i,j)
			a.put(i,j,a.get(j,i))
			a.put(j,i,t)
		}
	}
}

//go:noinline -- Redundant, but future-proofing against inline of FOR loops
func (a SqMat)copy() SqMat {
	n := len(a) // will assume square and fully populated
	b := matrix(n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			b.put(i,j,a.get(i,j))
		}
	}
	return b
}

// diagGets assigns x to diagonal d of a, where center = 0, rows below are negative, above are positive
//go:noinline -- Redundant, but future-proofing against inline of FOR loops
func diagGets(a SqMat, d int, x float64) {
	n := len(a) // will assume square and fully populated
	if d >= n || d <= -n {
		return
	}
	if d >= 0 {
		for i := d; i < n; i++ {
			a.put(i-d,i,x)
		}
	} else {
		for i := -d; i < n; i++ {
			a.put(i,i+d,x)
		}
	}
}

// rowGets assigns x to row k of a
//go:noinline -- Redundant, but future-proofing against inline of FOR loops
func rowGets(a SqMat, k int, x float64) {
	n := len(a) // will assume square and fully populated
	if k < 0 || k >= n {
		return
	}
	for j := 0; j < n; j++ {
		a.put(k,j,x)
	}
}

// colGets assigns x to column k of a
//go:noinline -- Redundant, but future-proofing against inline of FOR loops
func colGets(a SqMat, k int, x float64) {
	n := len(a) // will assume square and fully populated
	if k < 0 || k >= n {
		return
	}
	for j := 0; j < n; j++ {
		a.put(j,k,x)
	}
}

const N = 512

func main() {
	cpuprofile := "foo.prof"
	if cpuprofile != "" {
		file, _ := os.Create(cpuprofile)
		pprof.StartCPUProfile(file)
		defer func() {
			pprof.StopCPUProfile()
			file.Close()
		} ()
	}

	a := matrix(N)
	b := matrix(N)

	for k := 0; k < N; k++ {
		for i := 0; i < N; i++ {
			x := float64(i)
			diagGets(a, i, x)
			rowGets(a, i, x)
			colGets(a, i, x)
			diagGets(b, i, -x)
			rowGets(b, i, -x)
			colGets(b, i, -x)
			diagGets(b, i, -x)
			diagGets(a, -i, x)
		}
		transpose(a)
		transpose(b)
	}
}