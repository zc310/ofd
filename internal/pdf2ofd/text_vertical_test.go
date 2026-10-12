package pdf2ofd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	fontparser "github.com/tdewolff/font"
	"golang.org/x/image/font/gofont/goregular"
)

func verticalTextPDF(t *testing.T, content, metrics string, encodingCMap ...string) []byte {
	t.Helper()
	sfnt, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	mapData := make([]byte, 66)
	for code, r := range map[int]rune{1: 'A', 2: 'B', 3: 'C', 4: 'D', 32: ' '} {
		binary.BigEndian.PutUint16(mapData[code*2:], sfnt.GlyphIndex(r))
	}
	cmap := "begincmap 1 begincodespacerange <0000> <FFFF> endcodespacerange 5 beginbfchar <0001> <0041> <0002> <0042> <0003> <0043> <0004> <0044> <0020> <0020> endbfchar endcmap"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /GoRegular /Encoding /Identity-V /DescendantFonts [6 0 R] /ToUnicode 9 0 R >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GoRegular /DW 1000 /W [1 [600 1000 800] 32 [500]] " + metrics + " /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap 10 0 R >>",
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /Ascent 1000 /Descent -200 /FontBBox [0 -200 1100 1000] /CapHeight 800 /ItalicAngle 0 /StemV 80 /FontFile2 8 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(goregular.TTF), len(goregular.TTF), goregular.TTF),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(mapData), mapData),
	}
	if len(encodingCMap) > 0 {
		objects[4] = strings.Replace(objects[4], "/Encoding /Identity-V", "/Encoding 11 0 R", 1)
		objects = append(objects, fmt.Sprintf("<< /Type /CMap /CMapName /TestVertical /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /WMode 1 /Length %d >>\nstream\n%s\nendstream", len(encodingCMap[0]), encodingCMap[0]))
	}
	return assemblePDF(objects)
}

func TestVerticalEncodingCMapUsesCIDMetricsAndGlyphs(t *testing.T) {
	cmap := "begincmap /WMode 1 def 1 begincodespacerange <0000> <FFFF> endcodespacerange 1 begincidchar <0001> 2 endcidchar 1 begincidrange <0002> <0003> 3 endcidrange endcmap"
	pdf := verticalTextPDF(t, "BT /F1 25 Tf 80 160 Td <000100020003> Tj ET", "/W2 [2 [-1000 400 800 -1200 300 900 -900 500 700]]", cmap)
	doc, page, _ := textFidelityConvert(t, pdf)
	texts := layerTexts(page.Content().Layer[0])
	sfnt, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range [][2]float64{{70, 60}, {72.5, 87.5}, {67.5, 112.5}} {
		x, y := textModelOrigin(texts[i])
		if math.Abs(x-want[0]) > .002 || math.Abs(y-want[1]) > .002 {
			t.Fatalf("mapped CID origin %d=(%g,%g), want %v", i, x, y, want)
		}
		if len(texts[i].CGTransform) != 1 || len(texts[i].CGTransform[0].Glyphs) != 1 || texts[i].CGTransform[0].Glyphs[0] != int(sfnt.GlyphIndex(rune('B'+i))) {
			t.Fatalf("CIDToGIDMap used character code instead of CID: %+v", texts[i].CGTransform)
		}
	}
	textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
}

func TestVerticalOneByteEncodingCMapAppliesWordSpacing(t *testing.T) {
	cmap := "begincmap /WMode 1 def 1 begincodespacerange <00> <FF> endcodespacerange 3 begincidchar <01> 2 <02> 3 <20> 32 endcidchar endcmap"
	pdf := verticalTextPDF(t, "BT /F1 25 Tf 2 Tc 5 Tw 80 160 Td <012002> Tj ET", "/W2 [2 3 -1000 400 800]", cmap)
	doc, page, _ := textFidelityConvert(t, pdf)
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) != 2 {
		t.Fatalf("one-byte CMap texts=%d", len(texts))
	}
	x, y := textModelOrigin(texts[1])
	if math.Abs(x-70) > .002 || math.Abs(y-101) > .002 {
		t.Fatalf("one-byte Tw origin=(%g,%g), want (70,101)", x, y)
	}
	textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
}

func TestVerticalTextW2DW2OriginsSpacingAndTJ(t *testing.T) {
	pdf := verticalTextPDF(t, "BT /F1 20 Tf 50 Tz 2 Tc 50 Tw 80 160 Td [<00010002> 200 <0003000400200001>] TJ ET", "/DW2 [950 -1300] /W2 [1 [-1200 300 700 -900 500 800] 3 3 -1100 400 900]")
	doc, page, _ := textFidelityConvert(t, pdf)
	texts := layerTexts(page.Content().Layer[0])
	want := [][2]float64{{77, 54}, {75, 78}, {76, 100}, {75, 121}, {77, 164}}
	if len(texts) != len(want) {
		t.Fatalf("vertical texts=%d, want %d", len(texts), len(want))
	}
	for i, text := range texts {
		x, y := textModelOrigin(text)
		if math.Abs(x-want[i][0]) > .002 || math.Abs(y-want[i][1]) > .002 {
			t.Fatalf("glyph %d origin=(%g,%g), want %v", i, x, y, want[i])
		}
	}
	textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
}

func TestVerticalTextDefaultMetricsAndTextRise(t *testing.T) {
	pdf := verticalTextPDF(t, "BT /F1 20 Tf 150 Tz 5 Ts 80 160 Td <000100020003> Tj ET", "")
	_, page, _ := textFidelityConvert(t, pdf)
	texts := layerTexts(page.Content().Layer[0])
	for i, want := range [][2]float64{{71, 52.6}, {65, 72.6}, {68, 92.6}} {
		x, y := textModelOrigin(texts[i])
		if math.Abs(x-want[0]) > .002 || math.Abs(y-want[1]) > .002 {
			t.Fatalf("default metric glyph %d=(%g,%g), want %v", i, x, y, want)
		}
	}
}

func TestVerticalTextAffineBasisAndClipping(t *testing.T) {
	for _, matrix := range []string{"1 0 .25 1 90 170", "0 1 -1 0 20 60", "-1 0 0 1 110 170"} {
		t.Run(matrix, func(t *testing.T) {
			pdf := verticalTextPDF(t, "q BT /F1 35 Tf 7 Tr "+matrix+" Tm <000100020003> Tj ET 0 0 1 rg 0 0 200 200 re f Q", "/W2 [1 3 -1100 500 880]")
			doc, page, _ := textFidelityConvert(t, pdf)
			paths := layerPaths(page.Content().Layer[0])
			if len(paths) != 1 || paths[0].Clips == nil {
				t.Fatal("vertical glyph clipping missing")
			}
			textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
		})
	}
}

func TestVerticalMetricsIndirectArraysAndInvalidRanges(t *testing.T) {
	ctx := &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{}}}
	ctx.Table[1] = &model.XRefTableEntry{Object: types.Array{types.Integer(940), types.Integer(-1250)}}
	ctx.Table[2] = &model.XRefTableEntry{Object: types.Array{types.Integer(-1200), types.Integer(350), types.Integer(800)}}
	ctx.Table[3] = &model.XRefTableEntry{Object: types.Array{types.Integer(1), *types.NewIndirectRef(2, 0)}}
	p := pdfInterpreter{ctx: ctx}
	f := pdfFontInfo{widths: map[int]float64{}, defaultW: 1000}
	p.loadCIDVerticalMetrics(&f, types.Dict{"DW2": *types.NewIndirectRef(1, 0), "W2": *types.NewIndirectRef(3, 0)})
	if f.defaultVY != 940 || f.defaultW1 != -1250 || f.verticalMetrics[1] != (pdfVerticalMetric{-1200, 350, 800}) {
		t.Fatalf("indirect metrics=%+v", f)
	}
	for _, metrics := range []string{"/DW2 [880]", "/W2 [1 [-1000 500]]", "/W2 [4 2 -1000 500 880]"} {
		t.Run(metrics, func(t *testing.T) {
			var out bytes.Buffer
			var ds []Diagnostic
			err := ConvertWithOptions(t.Context(), verticalTextPDF(t, "BT /F1 20 Tf <0001> Tj ET", metrics), &out, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { ds = append(ds, d) }})
			// 校验器可先拒绝损坏的规范条目；进入解释器时必须产生明确度量诊断。
			if err == nil || out.Len() != 0 {
				t.Fatalf("invalid W2 accepted: %v", err)
			}
			if errors.Is(err, ErrUnsupported) && (len(ds) == 0 || (!strings.Contains(ds[0].Code, "font.vertical") && ds[0].Code != "font.resource")) {
				t.Fatalf("unexpected diagnostic: %+v", ds)
			}
		})
	}
}

func TestVerticalMetricsExpansionUsesOperationBudget(t *testing.T) {
	options, _ := (Options{Limits: Limits{MaxOperations: 10}}).Normalize()
	c := &conversionControl{ctx: t.Context(), options: options}
	ctx := &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{}}}
	p := pdfInterpreter{ctx: ctx, control: c}
	font := pdfFontInfo{}
	p.loadCIDVerticalMetrics(&font, types.Dict{"W2": types.Array{types.Integer(0), types.Integer(65535), types.Integer(-1000), types.Integer(500), types.Integer(880)}})
	if !errors.Is(c.check(), ErrLimitExceeded) || len(font.verticalMetrics) > 10 {
		t.Fatalf("unbounded W2 expansion: %v %d", c.check(), len(font.verticalMetrics))
	}
}
