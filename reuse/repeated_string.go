// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reuse

type RepeatedString []string

func (c *RepeatedString) String() string {
	s := ""
	for i, v := range *c {
		if i > 0 {
			s += ","
		}
		s += v
	}
	return s
}

func (c *RepeatedString) Set(s string) error {
	*c = append(*c, s)
	return nil
}

func (c *RepeatedString) IsBoolFlag() bool {
	return false
}
