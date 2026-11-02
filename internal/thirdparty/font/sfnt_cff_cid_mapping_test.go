package font

import (
	"testing"
)

func TestCFFGlyphCIDKeepsNonidentityCharsetAndFontDicts(t *testing.T) {
	data, err := readFontFixture(t, "resources/cid-cff-subset.otf")
	if err != nil {
		t.Fatal(err)
	}
	sfnt, err := ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	registry, ordering, _, cidKeyed := sfnt.CFF.CIDSystemInfo()
	if !cidKeyed || registry != "Adobe" || ordering == "" || sfnt.CFF.NumFontDicts() < 2 {
		t.Fatalf("CID metadata lost: %s/%s, dictionaries=%d", registry, ordering, sfnt.CFF.NumFontDicts())
	}
	different := false
	for _, cid := range []uint16{19, 97, 1063, 1720, 2169, 4605} {
		glyph := sfnt.GlyphIndex(rune(0xF0000 + int(cid)))
		if glyph == 0 {
			continue
		}
		if got, ok := sfnt.CFF.GlyphCID(glyph); !ok || got != cid {
			t.Fatalf("glyph %d CID=%d (%v), want %d", glyph, got, ok, cid)
		}
		different = different || glyph != cid
	}
	if !different {
		t.Fatal("fixture does not exercise nonidentity CID to GID")
	}
	if _, ok := sfnt.CFF.GlyphCID(sfnt.NumGlyphs()); ok {
		t.Fatal("out-of-range glyph accepted")
	}
	if cid, ok := sfnt.CFF.GlyphCID(0); !ok || cid != 0 {
		t.Fatal(".notdef CID must be zero")
	}
	// Full embedding must preserve CFF bytes and the original CID assignments.
	reparsed, err := ParseSFNT(sfnt.Write(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for glyph := uint16(0); glyph < sfnt.NumGlyphs(); glyph++ {
		before, _ := sfnt.CFF.GlyphCID(glyph)
		after, ok := reparsed.CFF.GlyphCID(glyph)
		if !ok || before != after {
			t.Fatalf("write changed glyph %d CID from %d to %d", glyph, before, after)
		}
	}
}
