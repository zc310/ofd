package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestIntroEmbeddedFontsLoad(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	fonts := NewFonts(ofd.Documents[0])
	for _, id := range []uint16{128, 396} {
		family, err := fonts.LoadFont(models.StRefID(id))
		if err != nil {
			t.Fatalf("font %d: %v", id, err)
		}
		if family == defaultFontFamily || !fontFamilyUsable(family) {
			t.Fatalf("font %d was not loaded as an embedded usable font", id)
		}
	}
	family, err := fonts.LoadFont(models.StRefID(128))
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(1, canvas.Black)
	for _, glyphID := range []uint16{4947, 3773, 6809} {
		if got := face.Font.GlyphIndex(fontfix.GlyphRune(glyphID)); got == 0 {
			t.Fatalf("CFF CID %d has no glyph mapping", glyphID)
		}
		path, _ := face.ToPath(string(fontfix.GlyphRune(glyphID)))
		if path == nil || path.Empty() {
			t.Fatalf("CFF glyph %d has no path", glyphID)
		}
	}
}

func TestAnoFont115UsesDeclaredEmbeddedFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	if got := string(doc.FontRes[115].FontFile); got != "Doc_0/Res/font_13132_0.ttf" {
		t.Fatalf("font 115 file = %q", got)
	}
	family, err := NewFonts(doc).LoadFont(115)
	if err != nil {
		t.Fatal(err)
	}
	if !fontFamilyUsable(family) {
		t.Fatal("font 115 is not usable")
	}
	if family == defaultFontFamily {
		t.Fatal("font 115 fell back to the default font")
	}
}

func TestAnoFont91MapsOFDGlyphs(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	family, err := NewFonts(ofd.Documents[0]).LoadFont(91)
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(1, canvas.Black)
	if path := directTextPath(face, string(fontfix.GlyphRune(2591))); path == nil || path.Empty() {
		t.Fatal("OFD glyph 2591 has no path")
	}
}

func TestAnoAnnotationFontUsesMatchingEmbeddedFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	family, err := NewFonts(doc).LoadFont(13134)
	if err != nil {
		t.Fatal(err)
	}
	if family == defaultFontFamily {
		t.Fatal("annotation font fell back to the default font")
	}
	face := family.Face(1, canvas.Black)
	for _, r := range "保密资料" {
		if got := face.Font.GlyphIndex(r); got == 0 {
			t.Fatalf("annotation character %q has no glyph", r)
		}
	}
	path, _ := face.ToPath("保")
	if path != nil && !path.Empty() {
		t.Fatal("annotation subset unexpectedly shaped Chinese text")
	}
	if path := directTextPath(face, "保密资料"); path == nil || path.Empty() {
		t.Fatal("annotation subset has no direct glyph path")
	}
}

func TestRepairFontDataProducesUsableEmbeddedFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	data, err := ofd.Documents[0].FileCache.Read("Doc_0/Res/font_13132.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := fontfix.Repair(data)
	if err != nil {
		t.Fatal(err)
	}
	family := canvas.NewFontFamily("test")
	if err := family.LoadFont(fixed, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	if !fontFamilyUsable(family) {
		t.Fatal("repaired font is not usable")
	}
	face := family.Face(1, canvas.Black)
	if got := face.Font.GlyphIndex(fontfix.GlyphRune(1)); got != 1 {
		t.Fatalf("repaired cmap maps glyph 1 to %d", got)
	}
}

func TestFallbackFontKeepsRegularAndBoldFaces(t *testing.T) {
	regular, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	bold, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans Bold is unavailable: %v", err)
	}

	fonts := NewFonts(nil)
	if err := fonts.AddFallbackFont(regular, "fallback", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := fonts.AddFallbackFont(bold, "fallback", canvas.FontBold); err != nil {
		t.Fatal(err)
	}

	regularFace := fonts.fallbacks["fallback"].Face(12, canvas.Black, canvas.FontRegular)
	boldFace := fonts.fallbacks["fallback"].Face(12, canvas.Black, canvas.FontBold)
	if regularFace == nil || boldFace == nil || regularFace.Font == boldFace.Font {
		t.Fatal("regular and bold fallback faces were not kept separately")
	}
}
