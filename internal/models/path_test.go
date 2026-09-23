package models

import (
	"strings"
	"testing"
)

func TestParsePathDataRejectsNonFiniteValues(t *testing.T) {
	for _, data := range []string{
		"M NaN 0",
		"L 0 +Inf",
		"A 1 2 NaN 0 0 3 4",
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := ParsePathData(data); err == nil {
				t.Fatal("expected non-finite path value to be rejected")
			}
		})
	}
}

func TestSplitPathTokensMatchesFields(t *testing.T) {
	corpus := []string{
		"",
		"   \t\n ",
		"M 0 0 L 10 10 C",
		"M 0.5 -1.25 10 20 C",
		"  M  1  2  3  4  ",
		"A 5 6 0 1 0 7 8 9 10",
		"M 0 0\nL 1 1\tA 2 3 0\r0 0 4 5 C",
		"中文 0 1 M 2 3",
		"M 0\xc2\xa00 L 1 1 C",
	}
	for _, data := range corpus {
		want := strings.Fields(data)
		got := splitPathTokens(data)
		if got.len() != len(want) {
			t.Fatalf("splitPathTokens(%q).len() = %d, want %d", data, got.len(), len(want))
		}
		for i := range want {
			if got.get(i) != want[i] {
				t.Fatalf("splitPathTokens(%q)[%d] = %q, want %q", data, i, got.get(i), want[i])
			}
		}
	}
	if _, err := ParsePathData("M 10 20 30 40 50 60 C"); err != nil {
		t.Fatalf("implicit coords parse failed: %v", err)
	}
}
