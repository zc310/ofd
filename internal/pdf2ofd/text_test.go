package pdf2ofd

import (
	"testing"
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
