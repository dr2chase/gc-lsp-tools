// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reuse

import (
	"fmt"
	"strconv"
)

// Count is a flag.Value that is like a flag.Bool and a flag.Int.
// If used as -name, it increments the Count, but -name=x sets the Count.
// Used for verbose flag -v.
type Count int

func (c *Count) String() string {
	return fmt.Sprint(int(*c))
}

func (c *Count) Set(s string) error {
	switch s {
	case "true":
		*c++
	case "false":
		*c = 0
	default:
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid count %q", s)
		}
		*c = Count(n)
	}
	return nil
}

func (c *Count) Get() interface{} {
	return int(*c)
}

func (c *Count) IsBoolFlag() bool {
	return true
}

func (c *Count) IsCountFlag() bool {
	return true
}

type T struct {
	A, B int
}

func F(a, b int) *T {
	return &T{a, b}
}
