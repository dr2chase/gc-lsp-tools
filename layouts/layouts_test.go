package layouts_test

import (
	"github.com/dr2chase/gc-lsp-tools/layouts"
	"testing"
	"unsafe"
)

func expect(t *testing.T, layout1, layout2 func(s string) (x []layouts.Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int), input, wantOutput string, wantSize, wantPtrSpan, wantAlign int) {
	bytes, size, align, contAlign, ptrSpan, end := layout1(input)
	output := layouts.AsString(bytes)
	if err := layouts.Validate(bytes); err != nil {
		t.Errorf("For input %v, output %v, validate failed %v", input, output, err)
	}
	if end != len(input) {
		t.Errorf("end[%v] != len(input)[%v]", end, len(input))
	}
	if size != wantSize {
		t.Errorf("size[%v] != wantSize[%v]", size, wantSize)
	}
	if align != wantAlign {
		t.Errorf("align[%v] != wantAlign[%v]", align, wantAlign)
	}
	if ptrSpan != wantPtrSpan {
		t.Errorf("ptrSpan[%v] != wantPtrSpan[%v]", ptrSpan, wantPtrSpan)
	}
	if output != wantOutput {
		t.Errorf("output[%v] != wantOutput[%v]", output, wantOutput)
	}

	if layout2 == nil {
		return
	}

	bytes2, size2, align2, contAlign2, ptrSpan2, end2 := layout2(input)

	output2 := layouts.AsString(bytes2)

	if output2 != output {
		t.Errorf("output2[%v] != output[%v]", output2, output)
	}

	if size2 != size {
		t.Errorf("size2[%v] != size[%v]", size2, size)
	}
	if align2 != align {
		t.Errorf("align2[%v] != align[%v]", align2, align)
	}
	if contAlign2 != contAlign {
		t.Errorf("contAlign2[%v] != contAlign[%v]", contAlign2, contAlign)
	}
	if ptrSpan2 != ptrSpan {
		t.Errorf("ptrSpan2[%v] != ptrSpan[%v]", ptrSpan2, ptrSpan)
	}
	if end2 != end {
		t.Errorf("end2[%v] != end[%v]", end2, end)
	}

}

func measure(t *testing.T,
	layout func(s string) (x []layouts.Builtin, at int, maxAlign int, contAlign int, ptrSpan int, end int),
	input, wantOutput string, wantData, wantInterior, wantTrailing int) {
	bytes, _, align, _, _, end := layout(input)
	output := layouts.AsString(bytes)

	if err := layouts.Validate(bytes); err != nil {
		t.Errorf("For input %v, output %v, validate failed %v", input, output, err)
	}
	if end != len(input) {
		t.Errorf("end[%v] != len(input)[%v]", end, len(input))
	}
	if output != wantOutput {
		t.Errorf("output[%v] != wantOutput[%v]", output, wantOutput)
	}

	data, interior, trailing := layouts.Measure(bytes, align)

	if (data+interior+trailing)%align != 0 {
		t.Errorf("(data[%d]+interior[%d]+trailing[%d]) %% align[%d] != 0", data, interior, trailing, align)
	}

	if data != wantData {
		t.Errorf("data[%v] != wantData[%v]", data, wantData)
	}
	if interior != wantInterior {
		t.Errorf("interior[%v] != wantInterior[%v]", interior, wantInterior)
	}
	if trailing != wantTrailing {
		t.Errorf("trailing[%v] != wantTrailing[%v]", trailing, wantTrailing)
	}
}

type BP struct {
	b byte
	p *byte
}

type BI struct {
	b byte
	p int
}

func TestAssumptions(t *testing.T) {
	if unsafe.Sizeof(BP{}) != 16 {
		t.Errorf("Expected 8 byte alignment of pointers")
	}
	if unsafe.Sizeof(BI{}) != 16 {
		t.Errorf("Expected 8 byte alignment of int")
	}
}

func want(t *testing.T, what string, g, w int) {
	if g != w {
		t.Errorf("%s, want %d, got %d", what, w, g)
	}
}

func TestPadFor(t *testing.T) {
	want(t, "layouts.PadFor(12,4)", layouts.PadFor(12, 4), 0)
	want(t, "layouts.PadFor(13,4)", layouts.PadFor(13, 4), 3)
	want(t, "layouts.PadFor(13,2)", layouts.PadFor(13, 2), 1)
	want(t, "layouts.PadFor(13,1)", layouts.PadFor(13, 1), 0)
	want(t, "layouts.PadFor(12,1)", layouts.PadFor(12, 1), 0)
}

func TestOne(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "1", "1", 1, 0, 1)
}
func TestArray0(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "[0]8", "", 0, 0, 8)
}
func TestArray00(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{[0]41}", "1...", 4, 0, 4)
}
func TestArray1(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "[2]1", "11", 2, 0, 1)
}
func TestArray2(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "[2][3]1", "111111", 6, 0, 1)
}

func TestStruct1(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{1}", "1", 1, 0, 1)
}

func TestStruct2(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{11}", "11", 2, 0, 1)
}

func TestStruct3(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{12}", "1.2", 4, 0, 2)
}
func TestStruct3P(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{21}", "21.", 4, 0, 2)
}

func TestStruct3U(t *testing.T) {
	expect(t, layouts.Builtins.UnpadSuffix, layouts.Builtins.UnpadSuffix, "{21}", "21", 3, 0, 2)
}

func TestArrayStruct3U(t *testing.T) {
	expect(t, layouts.Builtins.UnpadSuffix, layouts.Builtins.UnpadSuffix, "[2]{21}", "21.21", 7, 0, 2)
}

func TestStruct4(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{2841IsSP}", "2......841...IsSP", 11*8, 11*8, 8)
}

func TestStruct4Sort(t *testing.T) {
	expect(t, layouts.Builtins.Sort, nil, "{2841IsSP}", "IsSP8421.", 10*8, 8*8, 8)
}

func TestMore(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "[2]{2841IsSP}", "2......841...IsSP2......841...IsSP", 2*11*8, 2*11*8, 8)
}

func TestMoreSort(t *testing.T) {
	expect(t, layouts.Builtins.Sort, nil, "[2]{2841IsSP}", "IsSP8421.IsSP8421.", 2*10*8, 10*8+8*8, 8)
}

func TestGP1(t *testing.T) {
	expect(t, layouts.GpBuiltins.Plain, layouts.GpBuiltins.Plain, "S", "S........", 32, 8, 16)
}

func TestGP1U(t *testing.T) {
	expect(t, layouts.GpBuiltins.UnpadSuffix, layouts.GpBuiltins.UnpadSuffixAlt, "S", "S", 24, 8, 16)
}

func TestGP2(t *testing.T) {
	expect(t, layouts.GpBuiltins.Plain, layouts.GpBuiltins.Plain, "s", "s", 16, 8, 16)
}

func TestGP3(t *testing.T) {
	expect(t, layouts.GpBuiltins.Plain, layouts.GpBuiltins.Plain, "I", "I", 16, 8, 16)
}

func TestGP3iA(t *testing.T) {
	expect(t, layouts.GpBuiltins.Plain, layouts.GpBuiltins.Plain, "{I1}", "I1...............", 32, 8, 16)
}

func TestGP3iB(t *testing.T) {
	expect(t, layouts.GpBuiltins.UnpadSuffix, layouts.GpBuiltins.UnpadSuffixAlt, "{I1}", "I1", 17, 8, 16)
}

func TestGP4(t *testing.T) {
	expect(t, layouts.GpBuiltins.UnpadSuffix, layouts.GpBuiltins.UnpadSuffixAlt, "{S1}", "S1", 25, 8, 16)
}

func TestGPSortFill1(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{S}", "S", 24, 8, 16)
}

func TestGPSortFill2(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{S11}", "S11", 26, 8, 16)
}

func TestGPSortFill3(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{11S}", "S11", 26, 8, 16)
}

func TestGPSortFill4(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{S[2]{12}}", "S21.21", 31, 8, 16)
}

func TestGPSortFill5(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{1S[2]{12}}", "S21.211", 32, 8, 16)
}

func TestGPSortFill6(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{SSSS8[2]44[2]2[3]211}", "S8S44S224S11222", 128, 104, 16)
}

func TestGPSortFill8(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{{SS}{SS}8[2]44[2]2[3]211}", "S........S8S........S4442222211", 144, 104, 16)
}

func TestEmpty(t *testing.T) {
	expect(t, layouts.Builtins.Plain, layouts.Builtins.PlainAlt, "{888PI88{{}4}}", "888PI884....", 9*8, 40, 8)
}

func TestEmptySort(t *testing.T) {
	expect(t, layouts.Builtins.SortFill, nil, "{888PI88{{}4}}", "PI888884", 9*8-4, 16, 8)
}

func TestGPEmptySort(t *testing.T) {
	expect(t, layouts.GpBuiltins.SortFill, nil, "{888PI88{{}4}}", "IP888884", 9*8-4, 24, 16)
}

func TestCPPlain1(t *testing.T) {
	expect(t, layouts.Compressed.Plain, layouts.Compressed.PlainAlt, "Q", "Q", 4, 4, 4)
}
func TestCPPlain2(t *testing.T) {
	expect(t, layouts.Compressed.PlainAlt, layouts.Compressed.Plain, "{Q}", "Q....", 8, 4, 8)
}
func TestCPPlain3(t *testing.T) {
	expect(t, layouts.Compressed.Plain, layouts.Compressed.PlainAlt, "{QQ}", "QQ", 8, 8, 8)
}
func TestCPPlain4(t *testing.T) {
	expect(t, layouts.Compressed.Plain, layouts.Compressed.PlainAlt, "{{Q}Q}", "Q....Q....", 16, 12, 8)
}
func TestCPPlain5(t *testing.T) {
	expect(t, layouts.Compressed.Plain, layouts.Compressed.PlainAlt, "{{[0]Q}1}", "1.......", 8, 0, 8)
}
func TestCPFill1(t *testing.T) {
	expect(t, layouts.Compressed.UnpadSuffix, layouts.Compressed.UnpadSuffixAlt, "{{Q}Q}", "QQ", 8, 8, 8)
}
func TestCPFill2(t *testing.T) {
	expect(t, layouts.Compressed.UnpadSuffix, layouts.Compressed.UnpadSuffixAlt, "{{[0]Q1}Q}", "1...Q", 8, 8, 8)
}
func TestCPSortFill1(t *testing.T) {
	expect(t, layouts.Compressed.SortFill, nil, "{{[0]Q1}Q}", "Q....1", 9, 4, 8)
}

func TestCPSortFill2(t *testing.T) {
	expect(t, layouts.Compressed.SortFill, nil, "{{[0]Q1}Q1244}", "Q....11244", 20, 4, 8)
}

func TestCPSortFill3(t *testing.T) {
	// Note that trailing fill of fill structs is not itself filled, though it could be.
	expect(t, layouts.Compressed.SortFill, nil, "{ {[0]Q1} Q1 {[0]21} {[0]412} {[0]421} }", "Q....111.21.21", 19, 4, 8)
	expect(t, layouts.Compressed.SortFill, nil, "{ Q {[0]Q1} {[0]421} {[0]421} {[0]21} 1}", "Q....111.21.21", 19, 4, 8)
}

func TestMeasure0a(t *testing.T) {
	measure(t, layouts.Builtins.Plain, "[0]1", "", 0, 0, 0)
}

func TestMeasure0b(t *testing.T) {
	measure(t, layouts.Builtins.Plain, "[0]4", "", 0, 0, 0)
}

func TestMeasure1a(t *testing.T) {
	measure(t, layouts.Builtins.Plain, "{21}", "21.", 3, 0, 1)
}

func TestMeasure1b(t *testing.T) {
	measure(t, layouts.Builtins.SortFill, "{21}", "21", 3, 0, 1)
}

func TestMeasure2(t *testing.T) {
	measure(t, layouts.Compressed.SortFill, "{{[0]Q1}Q}", "Q....1", 5, 4, 7)
}
