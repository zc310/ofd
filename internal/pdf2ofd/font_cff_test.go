package pdf2ofd

import (
	"os"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	fontparser "github.com/zc310/font"
	"github.com/zc310/fontfix"
)

func TestPDFCFFGlyphTransformsUseGID(t *testing.T) {
	data, err := os.ReadFile("../../testdata/pdf/腾讯官方ESG报告.pdf")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("PDF 测试文件不存在，跳过: %v", err)
		}
		t.Fatal(err)
	}
	ctx, err, _ := readPDFContext(t.Context(), data, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	font := pdfFontInfo{widths: map[int]float64{}}
	p.loadFont(&font, *types.NewIndirectRef(15602, 0))
	codes := []uint64{0x13, 0x11, 0x13, 0x16}
	transforms := pdfTextGlyphTransforms(codes, "2025", font)
	want := []int{9, 7, 9, 12}
	if len(transforms) != len(want) {
		t.Fatalf("CFF transforms = %d, want %d", len(transforms), len(want))
	}
	for index, transform := range transforms {
		if len(transform.Glyphs) != 1 || transform.Glyphs[0] != want[index] {
			t.Fatalf("transform %d = %+v, want GID %d", index, transform, want[index])
		}
	}
	for index, item := range []struct {
		cid  uint16
		name string
	}{{19, "two"}, {17, "zero"}, {22, "five"}} {
		gid := uint16(transforms[[]int{0, 1, 3}[index]].Glyphs[0])
		cid, ok := font.sfnt.CFF.GlyphCID(gid)
		if !ok || cid != item.cid {
			t.Fatalf("GID %d CID = %d, want %d", gid, cid, item.cid)
		}
		if name := font.sfnt.GlyphName(gid); name != item.name {
			t.Fatalf("CID %d glyph name = %q, want %q", item.cid, name, item.name)
		}
	}
}

func TestPDFDecodeUnknownCustomGlyphUsesPrivateGIDRune(t *testing.T) {
	font := pdfFontInfo{
		codeBytes: 1,
		codeToGID: map[uint32]uint16{54: 22},
	}
	text, codes := decodePDFText([]byte{54}, font)
	if want := string(fontfix.GlyphRune(22)); text != want {
		t.Fatalf("decoded custom glyph = %U, want private GID rune %U", []rune(text), []rune(want))
	}
	if len(codes) != 1 || codes[0] != 54 {
		t.Fatalf("codes = %v, want [54]", codes)
	}
}

func TestPDFPage2CFFGlyphMappingsUseUnicodeGIDs(t *testing.T) {
	data, err := os.ReadFile("../../testdata/pdf/腾讯官方ESG报告.pdf")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("PDF 测试文件不存在，跳过: %v", err)
		}
		t.Fatal(err)
	}
	ctx, err, _ := readPDFContext(t.Context(), data, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	font := pdfFontInfo{widths: map[int]float64{}}
	p.loadFont(&font, *types.NewIndirectRef(3934, 0))
	registry, ordering, supplement, cidFont := font.sfnt.CFF.CIDSystemInfo()
	t.Logf("CFF ROS=%s/%s/%d cid=%v FDs=%d", registry, ordering, supplement, cidFont, font.sfnt.CFF.NumFontDicts())
	transforms := pdfTextGlyphTransforms([]uint64{0x26, 0x34, 0x28}, "ESG", font)
	if len(transforms) != 3 {
		t.Fatalf("CFF transforms = %d, want 3", len(transforms))
	}
	mappings := make([]fontfix.GlyphMapping, len(transforms))
	for index, transform := range transforms {
		gid := uint16(transform.Glyphs[0])
		cid, _ := font.sfnt.CFF.GlyphCID(gid)
		if name := font.sfnt.GlyphName(gid); name != string([]rune("ESG")[index]) {
			t.Fatalf("CID %d GID %d name=%q, want %c", cid, gid, name, []rune("ESG")[index])
		}
		mappings[index] = fontfix.GlyphMapping{Rune: []rune("ESG")[index], Glyph: gid}
	}
	fixed, err := fontfix.RepairWithGlyphs(font.data, mappings)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := fontparser.ParseSFNT(fixed, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, mapping := range mappings {
		if got := repaired.GlyphIndex(mapping.Rune); got != mapping.Glyph {
			t.Fatalf("repaired cmap[%c] = GID %d, want %d", mapping.Rune, got, mapping.Glyph)
		}
	}
}

func TestGBT38540CIDCFFImportBuildsGlyphTransforms(t *testing.T) {
	data, err := os.ReadFile("../../testdata/pdf/standards/GBT_38540-2020.pdf")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("测试标准 PDF 不存在，跳过: %v", err)
		}
		t.Fatal(err)
	}
	ctx, err, _ := readPDFContext(t.Context(), data, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	font := pdfFontInfo{widths: map[int]float64{}}
	p.loadFont(&font, *types.NewIndirectRef(56, 0))
	if font.sfnt == nil || font.sfnt.CFF == nil {
		t.Fatal("CID CFF font was not parsed")
	}

	code := uint64(0x004a) // 标准封面左上角“ICS”中的首字符。
	text := pdfCodeUnicode(code, font)
	if text == "" {
		t.Fatal("CID CFF character has no Unicode mapping")
	}
	transforms := pdfTextGlyphTransforms([]uint64{code}, text, font)
	if len(transforms) != 1 || len(transforms[0].Glyphs) != 1 {
		t.Fatalf("CID CFF transforms = %+v, want one glyph", transforms)
	}
	cid, ok := font.sfnt.CFF.GlyphCID(uint16(transforms[0].Glyphs[0]))
	if !ok || cid != uint16(pdfCIDCode(code, font)) {
		t.Fatalf("glyph %d maps to CID %d, want CID %d", transforms[0].Glyphs[0], cid, pdfCIDCode(code, font))
	}
}

func TestGBTCustomCFFEncodingPreservesPrivateGlyphRune(t *testing.T) {
	data, err := os.ReadFile("../../testdata/pdf/standards/GBT_38540-2020.pdf")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("测试标准 PDF 不存在，跳过: %v", err)
		}
		t.Fatal(err)
	}
	ctx, err, _ := readPDFContext(t.Context(), data, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	font := pdfFontInfo{widths: map[int]float64{}, codeBytes: 1}
	p.loadFont(&font, *types.NewIndirectRef(59, 0))

	glyph := font.codeToGID[54]
	if glyph == 0 {
		t.Fatal("自定义 /G36 编码未解析到字形 ID")
	}
	if got := pdfCodeUnicode(54, font); got != "" {
		t.Fatalf("自定义 /G36 被误解码为 %q", got)
	}
	text, codes := decodePDFText([]byte{54}, font)
	if want := string(fontfix.GlyphRune(glyph)); text != want {
		t.Fatalf("自定义字形解码为 %U，期望 GID 私有码位 %U", []rune(text), []rune(want))
	}
	if len(codes) != 1 || codes[0] != 54 {
		t.Fatalf("字符编码 = %v，期望 [54]", codes)
	}
}
