package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	fontparser "github.com/zc310/ofd/internal/thirdparty/font"
)

func TestPDFMultiFDCFFSubsetsProgramAndUsesOriginalCIDs(t *testing.T) {
	path := os.Getenv("OFD_CFF_TEST_FONT")
	data := multiFDCFFFixture(t)
	if path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("缺少 CFF 测试字体: %s", path)
			}
			t.Fatal(err)
		}
	}
	family := canvas.NewFontFamily("MultiFDCFF")
	if err := family.LoadFont(data, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	font := family.Face(5, canvas.Black).Font
	if font.SFNT.CFF == nil || font.SFNT.CFF.NumFontDicts() < 2 {
		t.Skip("test font needs multiple CFF Font DICTs")
	}
	// Avoid a font-specific alphabet: glyphs are selected directly, including
	// the final glyph, whose original GID is far from its compact page code.
	glyphs := []uint16{0, 1, 2, font.SFNT.NumGlyphs() - 1}
	before := append([]byte(nil), font.SFNT.Tables["CFF "]...)
	expected, err := font.Subset(glyphs, fontparser.SubsetOptions{Tables: fontparser.KeepPDFTables})
	if err != nil {
		t.Fatal(err)
	}
	if expected.NumGlyphs() != uint16(len(glyphs)) || len(expected.Tables["CFF "]) >= len(before) {
		t.Fatal("多 FD CFF 未裁剪为选中的字形")
	}
	for i, glyph := range glyphs {
		want, _ := font.CFF.GlyphCID(glyph)
		if got, ok := expected.CFF.GlyphCID(uint16(i)); !ok || got != want {
			t.Fatalf("子集 GID %d 的 CID = %d (%v)，期望原 CID %d", i, got, ok, want)
		}
		if i > 0 {
			var sourcePath, subsetPath canvas.Path
			if err := font.GlyphPath(&sourcePath, glyph, 0, 0, 0, 1, fontparser.NoHinting); err != nil {
				t.Fatal(err)
			}
			if err := expected.GlyphPath(&subsetPath, uint16(i), 0, 0, 0, 1, fontparser.NoHinting); err != nil {
				t.Fatal(err)
			}
			if sourcePath.String() != subsetPath.String() || font.GlyphAdvance(glyph) != expected.GlyphAdvance(uint16(i)) {
				t.Fatalf("原 GID %d 子集化后轮廓或字宽改变", glyph)
			}
		}
	}
	var previous []byte
	for repeat := 0; repeat < 2; repeat++ {
		var out bytes.Buffer
		writer := newPDFWriter(&out)
		writer.SetCompression(false)
		writer.NewPage(100, 100)
		ref := writer.getFont(font, false)
		for _, glyph := range glyphs {
			writer.fontSubset[font].Get(glyph)
		}
		writer.writeFont(ref, font, false)
		if writer.err != nil {
			t.Fatal(writer.err)
		}
		if !bytes.Contains(out.Bytes(), expected.Tables["CFF "]) || bytes.Contains(out.Bytes(), before) || !bytes.Equal(before, font.Tables["CFF "]) {
			t.Fatal("PDF 未使用多 FD CFF 子集，或修改了原始字体")
		}
		if !bytes.Contains(out.Bytes(), []byte("begincidchar")) || bytes.Contains(out.Bytes(), []byte("/CIDToGIDMap")) {
			t.Fatal("CIDFontType0 needs an Encoding CMap rather than CIDToGIDMap")
		}
		for i, glyph := range glyphs {
			cid, _ := font.CFF.GlyphCID(glyph)
			if !bytes.Contains(out.Bytes(), fmt.Appendf(nil, "<%04X> %d\n", i, cid)) {
				t.Fatalf("页内字符码 %d 未映射到原 CID %d", i, cid)
			}
		}
		if repeat > 0 && !bytes.Equal(previous, out.Bytes()) {
			t.Fatal("reusing the same font changed PDF embedding")
		}
		previous = append([]byte(nil), out.Bytes()...)
		latin := canvas.NewFontFamily("Latin")
		if err := latin.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
			t.Fatal(err)
		}
		latinFont := latin.Face(5, canvas.Black).Font
		latinRef := writer.getFont(latinFont, false)
		writer.fontSubset[latinFont].Get(latinFont.SFNT.GlyphIndex('A'))
		writer.writeFont(latinRef, latinFont, false)
		if writer.err != nil || bytes.Contains(out.Bytes(), latinFont.SFNT.Write()) || !writer.subset {
			t.Fatalf("CFF 子集化影响了 TrueType 子集化: %v", writer.err)
		}
	}
	// 显式关闭子集化时仍应保留完整程序，不能无条件裁剪字体。
	var full bytes.Buffer
	writer := newPDFWriter(&full)
	writer.SetCompression(false)
	writer.SetFontSubsetting(false)
	writer.NewPage(100, 100)
	ref := writer.getFont(font, false)
	writer.fontSubset[font].Get(glyphs[len(glyphs)-1])
	writer.writeFont(ref, font, false)
	if writer.err != nil || !bytes.Contains(full.Bytes(), before) {
		t.Fatalf("关闭子集化后未保留完整 CFF: %v", writer.err)
	}
}

func TestPDFEmbeddingDoesNotMutateCFFGlyphNames(t *testing.T) {
	family := canvas.NewFontFamily("CFF")
	loadExternalCFFTestFont(t, family)
	font := family.Face(5, canvas.Black).Font
	glyph := font.SFNT.GlyphIndex('A')
	name, _ := font.SFNT.CFF.GlyphName(glyph)
	var previous []byte
	for repeat := 0; repeat < 2; repeat++ {
		var out bytes.Buffer
		writer := newPDFWriter(&out)
		writer.SetCompression(false)
		writer.NewPage(100, 100)
		ref := writer.getFont(font, false)
		writer.fontSubset[font].Get(glyph)
		writer.writeFont(ref, font, false)
		if writer.err != nil {
			t.Fatal(writer.err)
		}
		if after, _ := font.SFNT.CFF.GlyphName(glyph); after != name {
			t.Fatalf("CFF glyph name changed from %q to %q", name, after)
		}
		if repeat > 0 && !bytes.Equal(previous, out.Bytes()) {
			t.Fatal("repeated CFF subsetting changed output")
		}
		previous = append([]byte(nil), out.Bytes()...)
	}
}

func TestPDFSubsettingErrorReturnsInsteadOfPanicking(t *testing.T) {
	family := canvas.NewFontFamily("Invalid")
	loadExternalCFFTestFont(t, family)
	font := family.Face(5, canvas.Black).Font
	var out bytes.Buffer
	writer := newPDFWriter(&out)
	writer.NewPage(100, 100)
	writer.getFont(font, false)
	writer.fontSubset[font].Get(font.SFNT.NumGlyphs())
	// An out-of-range charstring causes a bounded Subset error. The PDF writer
	// must preserve it through Close, not silently embed a broken font program.
	if err := writer.Close(); err == nil || !strings.Contains(err.Error(), "CFF: bad charString") {
		t.Fatalf("Close lost the subsetting error: %v", err)
	}
}

func loadExternalCFFTestFont(t *testing.T, family *canvas.FontFamily) {
	t.Helper()
	path := os.Getenv("OFD_CFF_SINGLE_FONT")
	if path == "" {
		t.Skip("set OFD_CFF_SINGLE_FONT to an external single-DICT CFF font")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("CFF test font unavailable: %s", path)
		}
		t.Fatal(err)
	}
	if err := family.LoadFont(data, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
}
