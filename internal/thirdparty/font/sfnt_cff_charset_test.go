package font

import (
	"testing"

	"github.com/tdewolff/test"
)

// TestCFFWriteGeneratesCharsetWhenNamesCleared covers fonts exceeding the predefined
// charset after callers clear glyph names with SetGlyphNames(nil). These fonts
// previously failed with "charset must be set explicitly for more than 229 glyphs",
// preventing embedding and forcing text to non-copyable vector outlines.
func TestCFFWriteGeneratesCharsetWhenNamesCleared(t *testing.T) {
	b, err := readFontFixture(t, "resources/EBGaramond12-Regular.otf")
	test.Error(t, err)

	sfnt, err := ParseSFNT(b, 0)
	test.Error(t, err)
	if sfnt.CFF == nil || sfnt.CFF.top.IsCID {
		t.Fatal("expected a non-CID CFF font")
	}
	if sfnt.NumGlyphs() <= 229 {
		t.Fatalf("fixture needs >229 glyphs, got %d", sfnt.NumGlyphs())
	}

	sfnt.CFF.SetGlyphNames(nil)

	out, err := sfnt.CFF.Write()
	test.Error(t, err)
	if len(out) == 0 {
		t.Fatal("empty CFF output")
	}
}
