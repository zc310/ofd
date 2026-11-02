package font

import (
	"fmt"
	"testing"
)

// Adobe 5176 sections 13 and 18 define numeric CIDs instead of name SIDs in a
// CID-keyed charset. Unnamed high CIDs must not collapse into one empty-string SID.
func TestCIDCFFSubsetPreservesNumericCharsetAcrossReordering(t *testing.T) {
	data, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	if err != nil {
		t.Fatal(err)
	}
	original, err := ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	glyphs := []uint16{0}
	want := []uint16{0}
	for _, cid := range []uint16{4605, 19, 2169, 97, 1720, 1063} {
		glyph := original.GlyphIndex(rune(0xF0000 + int(cid)))
		if glyph == 0 {
			t.Fatalf("fixture lacks CID %d", cid)
		}
		glyphs = append(glyphs, glyph)
		want = append(want, cid)
	}
	subset, err := original.Subset(glyphs, SubsetOptions{Tables: KeepMinTables})
	if err != nil {
		t.Fatal(err)
	}
	check := func(face *SFNT, want []uint16) {
		t.Helper()
		if int(face.NumGlyphs()) != len(want) {
			t.Fatalf("subset glyph count = %d, want %d", face.NumGlyphs(), len(want))
		}
		seen := map[uint16]bool{}
		for glyph, cid := range want {
			got, ok := face.CFF.GlyphCID(uint16(glyph))
			if !ok || got != cid || seen[got] {
				t.Fatalf("GID %d has CID %d (%v), want %d, duplicate=%v", glyph, got, ok, cid, seen[got])
			}
			seen[got] = true
		}
	}
	check(subset, want)
	// PDF embedding may clear glyph names; numeric CID mappings must survive.
	subset.CFF.SetGlyphNames(nil)
	program, err := subset.CFF.Write()
	if err != nil {
		t.Fatal(err)
	}
	subset.Tables["CFF "] = program
	reparsed, err := ParseSFNT(subset.Write(), 0)
	if err != nil {
		t.Fatal(err)
	}
	check(reparsed, want)
	// Subset again in a different order, as when exporting an OFD subset to PDF.
	second, err := reparsed.Subset([]uint16{0, 3, 1}, SubsetOptions{Tables: KeepMinTables})
	if err != nil {
		t.Fatal(err)
	}
	check(second, []uint16{0, want[3], want[1]})
}

// Top DICT operands and INDEX offsets grow at different encoding boundaries.
// Exercise both while keeping the subset glyphs fixed.
func TestCIDCFFSubsetOffsetsSurviveTopDICTGrowth(t *testing.T) {
	data, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	if err != nil {
		t.Fatal(err)
	}
	original, err := ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for count := 0; count <= 48; count++ {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			face, cff, top := *original, *original.CFF, *original.CFF.top
			top.XUID = make([]int, count)
			for i := range top.XUID {
				top.XUID[i] = 100000
			}
			cff.top, face.CFF = &top, &cff
			subset, err := face.Subset([]uint16{0, 1, 2}, SubsetOptions{Tables: KeepMinTables})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseSFNT(subset.Write(), 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}
