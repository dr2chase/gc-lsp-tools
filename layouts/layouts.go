package layouts

import (
	"fmt"
	"sort"
)

type Builtin struct {
	size, align, containerAlign int
	name                        byte
	ptrSpan                     int
}

type BuiltinLayouts map[byte]*Builtin

func s2m(s []Builtin) BuiltinLayouts {
	m := BuiltinLayouts(make(map[byte]*Builtin))
	for i, b := range s {
		m[b.name] = &s[i]
	}
	return m
}

var pad = Builtin{1, 1, 1, '.', 0}

// TODO use these appropriately
var a8 = Builtin{0, 8, 8, 'o', 0}
var a16 = Builtin{0, 16, 16, 'x', 0}

// M, X, Y are for makemap*, makeslice, growslice

var Builtins = s2m([]Builtin{
	{1, 1, 1, 'U', 0},
	{1, 1, 1, '1', 0},
	{2, 2, 2, '2', 0},
	{4, 4, 4, '4', 0},
	{8, 8, 8, '8', 0},
	{8, 4, 4, 'c', 0},
	{16, 8, 8, 'C', 0},
	{8, 8, 8, 'P', 8},
	{8, 8, 8, 'Q', 8},
	{16, 8, 8, 'I', 8},
	{16, 8, 8, 's', 8},
	{24, 8, 8, 'S', 8},
	{8, 8, 8, 'M', 0},
	{8, 8, 8, 'X', 0},
	{8, 8, 8, 'Y', 0},
})

var Compressed = s2m([]Builtin{
	{1, 1, 1, 'U', 0},
	{1, 1, 1, '1', 0},
	{2, 2, 2, '2', 0},
	{4, 4, 4, '4', 0},
	{8, 8, 8, '8', 0},
	{8, 4, 4, 'c', 0},
	{16, 8, 8, 'C', 0},
	{8, 8, 8, 'P', 8},
	{4, 4, 8, 'Q', 4},
	{16, 8, 8, 'I', 8},
	{16, 8, 8, 's', 8},
	{24, 8, 8, 'S', 8},
	{8, 8, 8, 'M', 0},
	{8, 8, 8, 'X', 0},
	{8, 8, 8, 'Y', 0},
})

var GpBuiltins = s2m([]Builtin{
	{1, 1, 1, 'U', 0},
	{1, 1, 1, '1', 0},
	{2, 2, 2, '2', 0},
	{4, 4, 4, '4', 0},
	{8, 8, 8, '8', 0},
	{8, 4, 4, 'c', 0},
	{16, 8, 8, 'C', 0},
	{8, 8, 8, 'P', 8},
	{8, 8, 8, 'Q', 8},
	{16, 16, 16, 'I', 8},
	{16, 16, 16, 's', 8},
	{24, 16, 16, 'S', 8},
	{8, 8, 8, 'M', 0},
	{8, 8, 8, 'X', 0},
	{8, 8, 8, 'Y', 0},
})

// Like GP, but not aligning slices, just to see how much perturbing alignment costs.
var GpMinusBuiltins = s2m([]Builtin{
	{1, 1, 1, 'U', 0},
	{1, 1, 1, '1', 0},
	{2, 2, 2, '2', 0},
	{4, 4, 4, '4', 0},
	{8, 8, 8, '8', 0},
	{8, 4, 4, 'c', 0},
	{16, 8, 8, 'C', 0},
	{8, 8, 8, 'P', 8},
	{8, 8, 8, 'Q', 8},
	{16, 16, 16, 'I', 8},
	{16, 16, 16, 's', 8},
	{24, 16, 8, 'S', 8},
	{8, 8, 8, 'M', 0},
	{8, 8, 8, 'X', 0},
	{8, 8, 8, 'Y', 0},
})

func padToAlign(align int, at int, x []Builtin) (int, []Builtin) {
	if align == 0 {
		return at, x
	}
	for at&(align-1) != 0 {
		at++
		x = append(x, pad)
	}
	return at, x
}

func fail(format string, args ...any) {
	panic(fmt.Errorf(format, args...))
}

func AsString(x []Builtin) string {
	s := ""
	for _, b := range x {
		s += string(b.name)
	}
	return s
}

type Error struct {
	FieldIndex int
	Offset     int
	WantAlign  int
	GotAlign   int
	Offending  Builtin
}

func (e Error) Error() string {
	return fmt.Sprintf("type '%c' at index %d with offset %d, wanted align %d but got %d",
		e.Offending.name, e.FieldIndex, e.Offset, e.WantAlign, e.GotAlign)
}

func alignOf(i int) int {
	return i & -i
}

func Validate(x []Builtin) error {
	offset := 0
	for i, b := range x {
		s, a := b.size, b.align
		if offset&^-a != 0 {
			return Error{i, offset, a, alignOf(offset), b}
		}
		offset += s
	}
	return nil
}

// Measure determines how storage is/isn't wasted by a layout.
// Align can be larger than parts, if for example a compressed pointer is contained within.
func Measure(x []Builtin, maxAlign int) (data, interior, trailing int) {
	offset := 0
	for _, b := range x {
		if b.name == '.' {
			trailing++
		} else {
			interior += trailing
			trailing = 0
			data += b.size
		}
		maxAlign = max(maxAlign, b.align)
		offset += b.size
	}
	if offset > 0 {
		// could do clever arithmetic, or just do this
		for alignOf(offset) < maxAlign {
			offset++
			trailing++
		}
	}
	return
}

func (bl BuiltinLayouts) Plain(s string) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	return bl.OptionalPadSuffix(s, true)
}

func (bl BuiltinLayouts) UnpadSuffix(s string) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	return bl.OptionalPadSuffix(s, false)
}

func (bl BuiltinLayouts) OptionalPadSuffix(s string, padSuffix bool) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	count := 0  // for arrays
	repeat := 1 // generated by arrays
	inArray := false

	for end = 0; end < len(s); end++ {
		c := s[end]
		if inArray {
			if '0' <= c && c <= '9' {
				count = count*10 + int(c-'0')
			} else if c == ']' {
				repeat = repeat * count
				count = 0
				inArray = false
			} else {
				fail("In array, expecting to see 0-9 and ']' but saw %c instead", c)
			}
		} else if l := bl[c]; l != nil {
			a := l.align
			ac := l.containerAlign
			s := l.size
			at, x = padToAlign(a, at, x)
			maxAlign = max(maxAlign, a)
			contAlign = max(contAlign, ac)

			for repeat > 0 {
				if l.ptrSpan > 0 {
					ptrSpan = at + l.ptrSpan
				}
				at += s
				x = append(x, *l)
				// Policy: size consumed must be a multiple of alignment WITHIN ARRAYS
				if padSuffix || repeat > 1 {
					at, x = padToAlign(a, at, x)
				}
				repeat--
			}
			repeat = 1

		} else if c == '[' {
			inArray = true
		} else if c == '{' {
			sX, sAt, sAlign, scAlign, sPS, sEnd := bl.OptionalPadSuffix(s[end+1:], padSuffix)
			contAlign = max(contAlign, scAlign)
			sAlign = max(sAlign, scAlign)
			at, x = padToAlign(sAlign, at, x)
			end += sEnd + 1
			maxAlign = max(maxAlign, sAlign)
			for repeat > 0 {
				if sPS > 0 {
					ptrSpan = at + sPS
				}
				at += sAt
				x = append(x, sX...)
				// Policy: size consumed must be a multiple of alignment WITHIN ARRAYS
				if padSuffix || repeat > 1 {
					at, x = padToAlign(sAlign, at, x)
				}
				repeat--
			}
			repeat = 1

		} else if c == '}' {
			return
		} else {
			fail("scanning type, saw unexpected '%c'", c)
		}
	}
	return
}

type field struct {
	x         []Builtin
	size      int
	align     int
	contAlign int
	ptrSpan   int
	end       int
}

func (bl BuiltinLayouts) PlainAlt(s string) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	f := bl.SortAndOptionalPadSuffixInterior(s, false, true)
	return f.x, f.size, f.align, f.contAlign, f.ptrSpan, f.end
}

func (bl BuiltinLayouts) UnpadSuffixAlt(s string) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	f := bl.SortAndOptionalPadSuffixInterior(s, false, false)
	return f.x, f.size, f.align, f.contAlign, f.ptrSpan, f.end
}

func (bl BuiltinLayouts) Sort(s string) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	f := bl.SortAndOptionalPadSuffixInterior(s, true, true)
	return f.x, f.size, f.align, f.contAlign, f.ptrSpan, f.end
}

func (bl BuiltinLayouts) SortFill(s string) (x []Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int) {
	f := bl.SortAndOptionalPadSuffixInterior(s, true, false)
	return f.x, f.size, f.align, f.contAlign, f.ptrSpan, f.end
}

func PadFor(offset, align int) int {
	// e.g. 0100 -> 1100 -> 0011
	mask := ^-align
	return (align - (offset & mask)) & mask
}

func (f *field) PadAfter() int {
	return PadFor(f.size, f.align)
}

func (bl BuiltinLayouts) SortAndOptionalPadSuffixInterior(s string, sortFields, padSuffix bool) field {
	var x []Builtin
	var at int
	var maxAlign int
	var maxContAlign int
	var ptrSpan int
	var end int
	count := 0  // for arrays
	repeat := 1 // generated by arrays
	inArray := false

	for end = 0; end < len(s); end++ {
		c := s[end]
		if c == ' ' {
			continue // for the sanity of test writers, allow spaces
		}
		if inArray {
			if '0' <= c && c <= '9' {
				count = count*10 + int(c-'0')
			} else if c == ']' {
				repeat = repeat * count
				count = 0
				inArray = false
			} else {
				fail("In array, expecting to see 0-9 and ']' but saw %c instead", c)
			}
		} else if l := bl[c]; l != nil {
			a := l.align
			s := l.size
			at, x = padToAlign(a, at, x)

			maxContAlign = max(maxContAlign, l.containerAlign)
			maxAlign = max(maxAlign, a)
			for repeat > 0 {
				if l.ptrSpan > 0 {
					ptrSpan = at + l.ptrSpan
				}
				at += s
				x = append(x, *l)
				// Policy: size consumed must be a multiple of alignment WITHIN ARRAYS
				if padSuffix || repeat > 1 {
					at, x = padToAlign(a, at, x)
				}
				repeat--
			}
			return field{x, at, maxAlign, maxContAlign, ptrSpan, end + 1}

		} else if c == '[' {
			inArray = true
		} else if c == '{' {
			var fields []field
			end++
			for s[end] != '}' {
				if s[end] == ' ' {
					end++
					continue
				}
				sf := bl.SortAndOptionalPadSuffixInterior(s[end:], sortFields, padSuffix)
				fields = append(fields, sf)
				end += sf.end
			}

			if sortFields {
				// NB known that 16 has pointers.
				var f16, f8p, f8, f4p, f4, f2, f1 []field
				for _, sf := range fields {
					switch sf.align {
					case 16:
						f16 = append(f16, sf)
					case 8:
						if sf.ptrSpan > 0 {
							f8p = append(f8p, sf)
						} else {
							f8 = append(f8, sf)
						}
					case 4:
						if sf.ptrSpan > 0 {
							f4p = append(f4p, sf)
						} else {
							f4 = append(f4, sf)
						}
					case 2:
						f2 = append(f2, sf)
					default:
						f1 = append(f1, sf)
					}
				}
				fields = fields[:0]
				if padSuffix {
					fields = append(fields, f16...)
					fields = append(fields, f8p...)
					fields = append(fields, f4p...)
					fields = append(fields, f8...)
					fields = append(fields, f4...)
					fields = append(fields, f2...)
					fields = append(fields, f1...)
				} else {
					order := func(fs []field) {
						sort.SliceStable(fs, func(i, j int) bool {
							// For non-ptr, this reduces to smaller first.
							// For pointer containing, this reduces to smallest post-pointer size first.
							// prefer {...P}{P...} to {P...}{...P}
							trailDiff := (fs[i].size - fs[i].ptrSpan) - (fs[j].size - fs[j].ptrSpan)
							if trailDiff < 0 {
								return true
							}

							if trailDiff == 0 {
								return fs[i].size < fs[j].size
							}
							return false

						})
					}
					order(f16)
					order(f8p) // NB single pointers come first!
					order(f4p) // NB single pointers come first!
					order(f8)
					order(f4)
					order(f2)
					order(f1)

					moveOne := func(fs *[]field, i int) field {
						f := (*fs)[i]
						fields = append(fields, f)
						if i == 0 {
							*fs = (*fs)[1:]
						} else if i == len(*fs)-1 {
							*fs = (*fs)[:len(*fs)-1]
						} else {
							l := len(*fs)
							copy((*fs)[i:], (*fs)[i+1:])
							*fs = (*fs)[:l-1]
						}

						return f
					}

					tryMove := func(fs []field, slop0 int) ([]int, int) {
						if len(fs) == 0 {
							return nil, slop0
						}
						thisAlignMask := -fs[0].align
						slop := slop0 & thisAlignMask
						slop00 := slop
						var indices []int

						// greedy algorithm, try largest first
						for i := len(fs) - 1; i >= 0; i-- {
							f := fs[i]
							if f.size <= slop {
								slop -= f.size
								slop = slop & thisAlignMask // might not be aligned size
								indices = append(indices, i)
							}
						}
						if len(indices) == 0 {
							return indices, slop0
						}
						return indices, slop0 - (slop00 - slop)
					}

					for len(f16) > 0 {
						f := moveOne(&f16, 0)
						if slop := f.PadAfter(); slop > 0 {
							var s8p, s8, s4p, s4, s2, s1 []int
							s8p, slop = tryMove(f8p, slop)
							s4p, slop = tryMove(f4p, slop)
							s8, slop = tryMove(f8, slop)
							s4, slop = tryMove(f4, slop)
							s2, slop = tryMove(f2, slop)
							s1, slop = tryMove(f1, slop)
							for _, s := range s1 {
								moveOne(&f1, s)
							}
							for _, s := range s2 {
								moveOne(&f2, s)
							}
							for _, s := range s4 {
								moveOne(&f4, s)
							}
							for _, s := range s4p {
								moveOne(&f4p, s)
							}
							for _, s := range s8 {
								moveOne(&f8, s)
							}
							for _, s := range s8p {
								moveOne(&f8p, s)
							}
						}
					}

					for len(f8p) > 0 {
						f := moveOne(&f8p, 0)
						if slop := f.PadAfter(); slop > 0 {
							var s4p, s4, s2, s1 []int
							s4p, slop = tryMove(f4p, slop)
							s4, slop = tryMove(f4, slop)
							s2, slop = tryMove(f2, slop)
							s1, slop = tryMove(f1, slop)
							for _, s := range s1 {
								moveOne(&f1, s)
							}
							for _, s := range s2 {
								moveOne(&f2, s)
							}
							for _, s := range s4 {
								moveOne(&f4, s)
							}
							for _, s := range s4p {
								moveOne(&f4p, s)
							}
						}
					}

					// Do we prioritize pointer span or alignment?
					for len(f4p) > 0 {
						f := moveOne(&f4p, 0)
						if slop := f.PadAfter(); slop > 0 {
							var s2, s1 []int
							s2, slop = tryMove(f2, slop)
							s1, slop = tryMove(f1, slop)
							for _, s := range s1 {
								moveOne(&f1, s)
							}
							for _, s := range s2 {
								moveOne(&f2, s)
							}
						}
					}

					for len(f8) > 0 {
						f := moveOne(&f8, 0)
						if slop := f.PadAfter(); slop > 0 {
							var s4p, s4, s2, s1 []int
							s4p, slop = tryMove(f4p, slop)
							s4, slop = tryMove(f4, slop)
							s2, slop = tryMove(f2, slop)
							s1, slop = tryMove(f1, slop)
							for _, s := range s1 {
								moveOne(&f1, s)
							}
							for _, s := range s2 {
								moveOne(&f2, s)
							}
							for _, s := range s4 {
								moveOne(&f4, s)
							}
							for _, s := range s4p {
								moveOne(&f4p, s)
							}
						}
					}

					for len(f4) > 0 {
						f := moveOne(&f4, 0)
						if slop := f.PadAfter(); slop > 0 {
							var s2, s1 []int
							s2, slop = tryMove(f2, slop)
							s1, slop = tryMove(f1, slop)
							for _, s := range s1 {
								moveOne(&f1, s)
							}
							for _, s := range s2 {
								moveOne(&f2, s)
							}
						}
					}

					for len(f2) > 0 {
						f := moveOne(&f2, 0)
						if slop := f.PadAfter(); slop > 0 {
							var s1 []int
							s1, slop = tryMove(f1, slop)
							for _, s := range s1 {
								moveOne(&f1, s)
							}
						}
					}

					fields = append(fields, f1...)
				}
			}

			var sX []Builtin
			var sAt, sAlign, sContAlign, sPtrSpan, sEnd int
			for _, sf := range fields {
				fX, fAt, fAlign, fContAlign, fPtrSpan := sf.x, sf.size, sf.align, sf.contAlign, sf.ptrSpan
				sAt, sX = padToAlign(fAlign, sAt, sX)
				if fPtrSpan > 0 {
					sPtrSpan = sAt + fPtrSpan
				}
				sAt += fAt
				sAlign = max(sAlign, fAlign)
				sContAlign = max(sContAlign, fContAlign)
				sX = append(sX, fX...)
				if padSuffix {
					sAt, sX = padToAlign(fAlign, sAt, sX)
				}
			}

			sAlign = max(sAlign, sContAlign)

			if padSuffix || repeat > 1 {
				at, x = padToAlign(sAlign, at, x)
			}
			end += sEnd + 1

			maxAlign = max(maxAlign, sAlign)
			maxContAlign = max(maxContAlign, sContAlign)

			for repeat > 0 {
				if sPtrSpan > 0 {
					ptrSpan = at + sPtrSpan
				}
				at += sAt
				x = append(x, sX...)
				// Policy: size consumed must be a multiple of alignment WITHIN ARRAYS
				if padSuffix || repeat > 1 {
					at, x = padToAlign(sAlign, at, x)
				}
				repeat--
			}
			return field{x, at, maxAlign, maxContAlign, ptrSpan, end}

		} else if c == '}' {
			return field{x, at, maxAlign, maxContAlign, ptrSpan, end}
		} else {
			fail("scanning type, saw unexpected '%c'", c)
		}
	}
	return field{x, at, maxAlign, maxContAlign, ptrSpan, end}

}
