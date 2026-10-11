package pdf2ofd

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
)

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
		mapping map[uint16]string
	}
	done := make(chan outcome, 1)
	go func() { done <- outcome{parsePDFToUnicode([]byte(cmap))} }()

	var mapping map[uint16]string
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
