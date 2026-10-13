package models

import (
	"strings"
	"testing"
)

func BenchmarkParsePathData(b *testing.B) {
	data := "M 3,3 L 57,3 L 57,21 L 3,21 C " +
		"M 10 30 L 40 30 L 40 60 L 10 60 C " +
		"M 70 10 B 80 0 100 0 110 10 B 120 20 120 40 110 50 C"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParsePathData(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParsePathDataVariants(b *testing.B) {
	for _, tc := range []struct{ name, data string }{
		{"longLines", "M 0 0 " + strings.Repeat("L 10 20 ", 1000) + "C"},
		{"implicitLines", "M 0 0 " + strings.Repeat("10 20 ", 1000) + "C"},
		{"curves", "M 0 0 " + strings.Repeat("Q 1 2 3 4 B 5 6 7 8 9 10 ", 100) + "C"},
		{"arcs", "M 0 0 " + strings.Repeat("A 5 6 30 1 0 10 20 ", 100) + "C"},
		{"unicode", "M0\u00a00 " + strings.Repeat("L10\u300020", 100) + "C"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParsePathData(tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPathString(b *testing.B) {
	p, err := ParsePathData("M 3 3 L 57 3 L 57 21 L 3 21 C M 70 10 B 80 0 100 0 110 10 A 5 6 30 1 0 13 14 C")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = p.String()
	}
}
