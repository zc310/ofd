package pdf2ofd

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	fontparser "github.com/zc310/font"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

func TestConvertTextBoundaryContainsFontDescendersWithoutMovingBaseline(t *testing.T) {
	// 使用首页标题的字号、位置和黑体度量；单字 Tj 同时覆盖文字合并路径。
	for _, tc := range []struct {
		name       string
		descriptor string
		ascent     float64
		descent    float64
		textState  string
	}{
		{"title", "/Ascent 859 /Descent -156 /FontBBox [-12 -156 996 859]", 1, 0.156, "BT /F1 26.04 Tf 155.76012 493.5474 Td"},
		{"font bbox exceeds metrics", "/Ascent 900 /Descent -100 /FontBBox [-12 -300 996 1200]", 1.2, 0.3, "BT /F1 26.04 Tf 155.76012 493.5474 Td"},
		{"text matrix scale", "/Ascent 859 /Descent -156 /FontBBox [-12 -156 996 859]", 1, 0.156, "BT /F1 1 Tf 26.04 0 0 26.04 155.76012 493.5474 Tm"},
		{"ctm scale", "/Ascent 859 /Descent -156 /FontBBox [-12 -156 996 859]", 1, 0.156, "2 0 0 2 0 0 cm BT /F1 13.02 Tf 77.88006 246.7737 Td"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.textState + " <7535> Tj <5B50> Tj ET"
			objects := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595.32 841.92] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
				"<< /Type /Font /Subtype /Type0 /BaseFont /SimHei /Encoding /UniGB-UCS2-H /DescendantFonts [6 0 R] >>",
				"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /SimHei /DW 1000 /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> /FontDescriptor 7 0 R >>",
				"<< /Type /FontDescriptor /FontName /SimHei /Flags 4 /CapHeight 688 /ItalicAngle 0 /StemV 76 " + tc.descriptor + " >>",
			}
			page := parseConvertedPage(t, objects)
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 1 || len(texts[0].TextCode) != 1 || texts[0].TextCode[0].Value != "电子" {
				t.Fatalf("expected one merged title text, got %+v", texts)
			}
			text := texts[0]
			code := text.TextCode[0]
			size := 26.04 * pdfPointToMillimeter
			wantBaseline := 348.3726 * pdfPointToMillimeter
			if got := text.Boundary.Y + code.Y; math.Abs(got-wantBaseline) > 0.0002 {
				t.Fatalf("baseline = %g, want %g", got, wantBaseline)
			}
			if code.Y+0.0002 < size*tc.ascent {
				t.Fatalf("boundary clips ascent: baseline offset %g, ascent %g", code.Y, size*tc.ascent)
			}
			if got := text.Boundary.Height - code.Y; got+0.0002 < size*tc.descent {
				t.Fatalf("boundary clips descenders: space below baseline %g, descent %g", got, size*tc.descent)
			}
			if len(code.DeltaX) != 1 || math.Abs(code.DeltaX[0]-size) > 0.0002 {
				t.Fatalf("merged glyph advance = %v, want %g", code.DeltaX, size)
			}
		})
	}
}

func TestConvertTextBoundaryIncludesEmbeddedFontBounds(t *testing.T) {
	// 描述符刻意给出很小的范围，实际下伸范围必须取自内嵌字体。
	content := "BT /F1 20 Tf 30 100 Td (gj) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 0 /LastChar 255 /Widths [" + strings.Repeat("600 ", 256) + "] /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /Ascent 1 /Descent -1 /FontBBox [0 -1 1 1] /CapHeight 1 /ItalicAngle 0 /StemV 80 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(goregular.TTF), len(goregular.TTF), goregular.TTF),
	}
	page := parseConvertedPage(t, objects)
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) != 1 || len(texts[0].TextCode) != 1 || texts[0].TextCode[0].Value != "gj" {
		t.Fatalf("expected one embedded-font text, got %+v", texts)
	}
	sfnt, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	text := texts[0]
	code := text.TextCode[0]
	size := 20 * pdfPointToMillimeter
	wantDescent := -float64(sfnt.Head.YMin) / float64(sfnt.Head.UnitsPerEm) * size
	if got := text.Boundary.Height - code.Y; got+0.0002 < wantDescent {
		t.Fatalf("embedded glyph bounds clipped: space below baseline %g, want >= %g", got, wantDescent)
	}
	if got := text.Boundary.Y + code.Y; math.Abs(got-100*pdfPointToMillimeter) > 0.0002 {
		t.Fatalf("baseline moved to %g", got)
	}
}

func TestMergeTextPreservesExplicitOrigins(t *testing.T) {
	x, y := 0.0, 8.0
	first := creator.Text{X: 10, Y: 20, Width: 8, Height: 10, Size: 8, Font: "F", Value: "a", TextCodes: []creator.TextCode{{Value: "a", X: &x, Y: &y}}}
	second := first
	second.X = 18
	second.Value = "b"
	second.TextCodes = []creator.TextCode{{Value: "b", X: &x, Y: &y}}
	items := mergeAdjacentTextItems([]creator.Item{first, second})
	if len(items) != 1 {
		t.Fatalf("expected one merged text, got %d", len(items))
	}
	merged := items[0].(creator.Text)
	code := merged.TextCodes[0]
	if code.X == nil || *code.X != x || code.Y == nil || *code.Y != y {
		t.Fatalf("merged text lost explicit origin: %+v", code)
	}
	otherY := y + 1
	second.TextCodes = []creator.TextCode{{Value: "b", X: &x, Y: &otherY}}
	if got := len(mergeAdjacentTextItems([]creator.Item{first, second})); got != 2 {
		t.Fatalf("merged texts with different baselines: got %d items", got)
	}
}

func TestConvertSetsTextStyleFlagsFromFont(t *testing.T) {
	// 文字对象必须带上与字体一致的 Italic/Weight。否则阅读器按“常规”样式
	// 请求一个已加载为斜体的嵌入字体，会再叠加一次合成斜体，把字形压扁成
	// 斜线（annotTest.pdf 的 FreeText 曾出现该问题）。
	cases := []struct {
		baseFont string
		italic   bool
		weight   int
	}{
		{"Times-Italic", true, 0},
		{"Times-BoldItalic", true, 700},
		{"Times-Bold", false, 700},
		{"Times-Roman", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.baseFont, func(t *testing.T) {
			content := "BT /F1 12 Tf 10 10 Td (Text) Tj ET"
			objects := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
				"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
				"<< /Type /Font /Subtype /Type1 /BaseFont /" + tc.baseFont + " >>",
			}
			page := parseConvertedPage(t, objects)
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 1 {
				t.Fatalf("expected 1 text object, got %d", len(texts))
			}
			if texts[0].Italic != tc.italic {
				t.Fatalf("Italic = %v, want %v", texts[0].Italic, tc.italic)
			}
			if texts[0].Weight != tc.weight {
				t.Fatalf("Weight = %d, want %d", texts[0].Weight, tc.weight)
			}
		})
	}
}

func TestParsePDFToUnicodeRangeEndingAtMaxTerminates(t *testing.T) {
	// bfrange 的终点为 0xFFFF 时，code 是 uint16，递增会回绕成 0，
	// 循环条件恒真——basicapi.pdf 因此永远转不完，且 SIGTERM 无效。
	// 这里必须能返回，且区间内每个码位都有映射。
	cmap := "begincmap\n1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"1 beginbfrange\n<0020> <FFFF> <0041>\nendbfrange\nendcmap\n"

	type outcome struct {
		mapping map[uint32]string
	}
	done := make(chan outcome, 1)
	go func() { done <- outcome{parsePDFToUnicode([]byte(cmap))} }()

	var mapping map[uint32]string
	select {
	case got := <-done:
		mapping = got.mapping
	case <-time.After(10 * time.Second):
		t.Fatal("parsePDFToUnicode did not terminate: uint16 wraparound in the bfrange loop")
	}

	if got := mapping[0x20]; got != "A" {
		t.Fatalf("code 0x20 = %q, want %q", got, "A")
	}
	if got := mapping[0xFFFF]; got == "" {
		t.Fatal("code 0xFFFF has no mapping")
	}
	// 上界不可超过码位空间，否则畸形 CMap 能把内存撑到 GB 级。
	if len(mapping) > 1<<16 {
		t.Fatalf("expanded %d codes, want <= %d", len(mapping), 1<<16)
	}
}

func TestConvertUsesCoreFontWidthsWhenWidthsAbsent(t *testing.T) {
	// 基础 14 字体按规范不要求写 /Widths，阅读器必须自带度量。不补字宽时
	// 每个字符都回落到 defaultW（500），整段按等宽推进，与实际排版不符：
	// "Hello ASCII" 会被算成 27.16mm，正确值是 25.53mm。
	content := []byte("BT /F0 14 Tf 72 720 Td (Hello ASCII) Tj ET")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F0 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) == 0 {
		t.Fatal("no text object produced")
	}
	codes := texts[0].TextCode
	if len(codes) != 1 {
		t.Fatalf("text code count = %d, want 1", len(codes))
	}
	deltas := codes[0].DeltaX

	// Helvetica 的 AFM 字宽（千分之一 em）：H 722 e 556 l 222 l 222 o 556
	// 空格 278 A 667 S 667 C 722 I 278 I 278。
	want := []float64{722, 556, 222, 222, 556, 278, 667, 667, 722, 278}
	if len(deltas) != len(want) {
		t.Fatalf("DeltaX count = %d, want %d (uniform spacing means no core widths)", len(deltas), len(want))
	}
	total := 0.0
	for index, width := range want {
		expected := width / 1000.0 * 14.0 * pdfPointToMillimeter
		if math.Abs(deltas[index]-expected) > 0.002 {
			t.Fatalf("DeltaX[%d] = %.4f, want %.4f", index, deltas[index], expected)
		}
		total += deltas[index]
	}
	// 加上末字符 I 的推进量应逼近整段实际宽度 25.53mm；等宽回退会得到 27.16mm。
	total += 278.0 / 1000.0 * 14.0 * pdfPointToMillimeter
	if math.Abs(total-25.528) > 0.05 {
		t.Fatalf("total advance = %.3fmm, want ~25.528mm", total)
	}
}

func TestCoreFontNameStripsSubsetPrefix(t *testing.T) {
	// Type1 子集前缀固定 6 个字母加 '+'，去前缀后仍要认得出基础 14。
	cases := map[string]string{
		"Helvetica":            "Helvetica",
		"ABCDEF+Helvetica":     "Helvetica",
		"LTHEGL+FZHTK--GBK1-0": "",
		"Arial":                "",
		"Courier-Bold":         "Courier-Bold",
		"ABCDE+Helvetica":      "",
		"ABCDEFG+Times-Roman":  "",
	}
	for input, want := range cases {
		if got := coreFontName(input); got != want {
			t.Errorf("coreFontName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTokenizePDFCMapStopsAtTokenLimit(t *testing.T) {
	tokens, complete := tokenizePDFCMapLimit("1 2 3 4 5", 3)
	if complete || len(tokens) != 3 {
		t.Fatalf("tokens=%v complete=%v, want three tokens and incomplete", tokens, complete)
	}
	tokens, complete = tokenizePDFCMapLimit("1 2 3 4 5", 5)
	if !complete || len(tokens) != 5 {
		t.Fatalf("tokens=%v complete=%v, want all five tokens", tokens, complete)
	}
}

func TestPDFHexHelpersIgnoreWhitespaceAndPadOddNibbles(t *testing.T) {
	if got, ok := pdfHexUint("<00 41>"); !ok || got != 0x41 {
		t.Fatalf("pdfHexUint whitespace = %#x ok=%v", got, ok)
	}
	if got, ok := pdfHexUint("<1>"); !ok || got != 0x10 {
		t.Fatalf("pdfHexUint odd = %#x ok=%v", got, ok)
	}
	if got, ok := pdfHexString("<00 41>"); !ok || got != "A" {
		t.Fatalf("pdfHexString whitespace = %q ok=%v", got, ok)
	}
	if got, ok := pdfHexString("<4>"); !ok || got != "@" {
		t.Fatalf("pdfHexString odd = %q ok=%v", got, ok)
	}
	if got, length, ok := pdfEncodingHex("<0 041>"); !ok || got != 0x41 || length != 2 {
		t.Fatalf("pdfEncodingHex whitespace = %#x length=%d ok=%v", got, length, ok)
	}
	if got, length, ok := pdfEncodingHex("<1>"); !ok || got != 0x10 || length != 1 {
		t.Fatalf("pdfEncodingHex odd = %#x length=%d ok=%v", got, length, ok)
	}
}

func TestTextMergeRunTracksLeftmostOrigin(t *testing.T) {
	first := creator.Text{X: 10, Y: 20, Width: 2, Height: 10, Size: 8, Font: "F", Value: "a"}
	run := newTextMergeRun(first, textGlyphUnit{value: "a", codeLen: 1})
	second := creator.Text{X: 1, Y: 20, Width: 1, Height: 10, Size: 8, Font: "F", Value: "b"}
	run.append(second, textGlyphUnit{value: "b", codeLen: 1})
	merged := run.build()
	if merged.X != 1 || merged.Width != 11 {
		t.Fatalf("merged X=%g width=%g, want X=1 width=11", merged.X, merged.Width)
	}
}
