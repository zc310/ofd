package font

import "testing"

func TestCmapWriteFormat4KeepsZeroDeltaOutOfGlyphArray(t *testing.T) {
	sfnt := &SFNT{
		Maxp:   &maxpTable{NumGlyphs: 1},
		Tables: map[string][]byte{"cmap": cmapWrite([]rune{0}, map[rune]uint16{0: 0})},
	}
	if err := sfnt.parseCmap(); err != nil {
		t.Fatal(err)
	}
	if got := sfnt.Cmap.Get(0); got != 0 {
		t.Fatalf("U+0000 glyph = %d, want .notdef glyph 0", got)
	}
}
