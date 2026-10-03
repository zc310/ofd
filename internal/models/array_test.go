package models

import "testing"

func TestStArrayFExpandsRepeatedValues(t *testing.T) {
	var values StArrayF
	if err := values.parseString("1 g 3 2 3"); err != nil {
		t.Fatal(err)
	}
	want := StArrayF{1, 2, 2, 2, 3}
	if len(values) != len(want) {
		t.Fatalf("values = %v, want %v", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("values = %v, want %v", values, want)
		}
	}
}

// 无 g 展开标记的常见输入走快速路径，结果必须与逐 token 解析一致。
func TestStArrayFPlainSequence(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want StArrayF
	}{
		{"2 3", StArrayF{2, 3}},
		{"1.5 -0.5\t4e2\n7", StArrayF{1.5, -0.5, 400, 7}},
		{"  10  ", StArrayF{10}},
		{"", StArrayF{}},
	} {
		var values StArrayF
		if err := values.parseString(tc.in); err != nil {
			t.Fatalf("parseString(%q): %v", tc.in, err)
		}
		if len(values) != len(tc.want) {
			t.Fatalf("parseString(%q) = %v, want %v", tc.in, values, tc.want)
		}
		for i := range tc.want {
			if values[i] != tc.want[i] {
				t.Fatalf("parseString(%q) = %v, want %v", tc.in, values, tc.want)
			}
		}
	}
}

func TestStArrayFPlainSequenceRejectsMalformedFloat(t *testing.T) {
	for _, input := range []string{"1 nope", "NaN", "Inf", "1 2 3x"} {
		var values StArrayF
		if err := values.parseString(input); err == nil {
			t.Fatalf("input %q was accepted", input)
		}
	}
}

func TestStArrayFRejectsExcessiveRepeatCount(t *testing.T) {
	var values StArrayF
	if err := values.parseString("g 9223372036854775807 1"); err == nil {
		t.Fatal("expected excessive repeat count to be rejected")
	}
}

func TestStArrayFRejectsExcessiveExpandedLength(t *testing.T) {
	var values StArrayF
	if err := values.parseString("g 100000 1 g 100000 2 g 100000 3 g 100000 4 g 100000 5 g 100000 6 g 100000 7 g 100000 8 g 100000 9 g 100000 10 g 100000 11"); err == nil {
		t.Fatal("expected expanded array length to be rejected")
	}
}

func TestStArrayFRejectsMalformedRepeatSyntax(t *testing.T) {
	for _, input := range []string{"g", "g -1 1", "g x 1", "g 2 nope"} {
		var values StArrayF
		if err := values.parseString(input); err == nil {
			t.Fatalf("input %q was accepted", input)
		}
	}
}
