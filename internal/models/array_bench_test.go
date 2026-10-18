package models

import (
	"encoding/xml"
	"strings"
	"testing"
)

func BenchmarkStArrayFParse(b *testing.B) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"DashPattern", "2 3"},
		{"DeltaX", "1.5 0 0 0 1.5 0 0 0 1.5 0 0 0"},
		{"Repeat", "g 5 0"},
		{"LargeRepeat", "g 10000 1.5"},
		{"Long", "0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19"},
		{"LargeDeltaX", strings.Repeat("1.5 0 ", 512)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var s StArrayF
				if err := s.parseString(tc.in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStArrayIParse(b *testing.B) {
	for _, input := range []struct{ name, value string }{
		{"Glyphs", "12 34 56 78 90"},
		{"LongGlyphs", strings.Repeat("12 34 ", 512)},
	} {
		b.Run(input.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var values StArrayI
				if err := values.parseString(input.value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNumericAttributes(b *testing.B) {
	b.Run("Boundary", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var value StBox
			if err := value.parseFromString("1.5 2.5 30 40"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("CTM", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var value CTM
			if err := value.UnmarshalXMLAttr(xml.Attr{Value: "1 0 0 1 12.5 20.5"}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Color", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var value Color
			if err := value.parse("128 64 255"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
