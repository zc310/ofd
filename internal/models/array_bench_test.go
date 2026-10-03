package models

import "testing"

func BenchmarkStArrayFParse(b *testing.B) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"DashPattern", "2 3"},
		{"DeltaX", "1.5 0 0 0 1.5 0 0 0 1.5 0 0 0"},
		{"Repeat", "g 5 0"},
		{"Long", "0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19"},
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
