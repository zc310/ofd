package pdf

import (
	"bytes"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPDFToUnicodeKeepsSourceTextForSharedGlyphAndLigature(t *testing.T) {
	family := canvas.NewFontFamily("SourceText")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	font := family.Face(5, canvas.Black).Font
	var out bytes.Buffer
	writer := newPDFWriter(&out)
	writer.SetCompression(false)
	writer.NewPage(100, 100)
	ref := writer.getFont(font, false)
	encoding := writer.fontSubset[font]
	glyph := font.SFNT.GlyphIndex('A')
	first := encoding.Get(glyph, "A")
	alias := encoding.Get(glyph, "文")
	ligature := encoding.Get(glyph, "ffi")
	supplementary := encoding.Get(glyph, "𠀀")
	if first == alias || alias == ligature || first != encoding.Get(glyph, "A") || supplementary == first {
		t.Fatal("different source strings were merged into one PDF code")
	}
	writer.writeFont(ref, font, false)
	if writer.err != nil {
		t.Fatal(writer.err)
	}
	for _, mapping := range []string{"<0001> <0041>", "<0002> <6587>", "<0003> <006600660069>", "<0004> <D840DC00>"} {
		if !bytes.Contains(out.Bytes(), []byte(mapping)) {
			t.Fatalf("ToUnicode lost %s", mapping)
		}
	}
}

func TestPDFFontCodeLimitDoesNotWrap(t *testing.T) {
	encoding := newPDFFontSubsetter()
	for glyph := 1; glyph <= 65535; glyph++ {
		if code := encoding.Get(uint16(glyph)); code != uint16(glyph) {
			t.Fatalf("code=%d want %d", code, glyph)
		}
	}
	encoding.Get(1, "another character")
	if encoding.err == nil || len(encoding.List()) != 65536 {
		t.Fatal("PDF font code limit wrapped")
	}
}
