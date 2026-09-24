package pdf2ofd

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func parseConvertedPage(t *testing.T, objects []string) *parser.Page {
	t.Helper()
	var output bytes.Buffer
	if err := Convert(assemblePDF(objects), &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestConvertRendersAnnotationAppearance(t *testing.T) {
	// 注解的外观流 /AP /N 必须绘制到页面上，并按 BBox→Rect 变换放置，
	// 否则带注解的页面（高亮、图章、方框等）会缺失内容。
	form := "1 0 0 rg 0 0 40 40 re f"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << >> /Contents 4 0 R /Annots [5 0 R] >>",
		"<< /Length 3 >>\nstream\nq Q\nendstream",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 50 50] /F 4 /AP << /N 6 0 R >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 40 40] /Length " + itoa(len(form)) + " >>\nstream\n" + form + "\nendstream",
	}
	page := parseConvertedPage(t, objects)
	found := false
	for _, item := range layerPaths(page.Content().Layer[0]) {
		if item.FillColor == nil || item.FillColor.Value == nil {
			continue
		}
		value := item.FillColor.Value.RGBA
		if value.R == 255 && value.G == 0 && value.B == 0 {
			found = true
			// Rect [10 10 50 50] → 边界宽高约 40pt * 25.4/72 = 14.1mm。
			if item.Boundary.Width < 14 || item.Boundary.Width > 14.2 {
				t.Fatalf("annotation boundary width = %g, want ~14.11", item.Boundary.Width)
			}
		}
	}
	if !found {
		t.Fatal("annotation appearance stream was not rendered")
	}
}

func TestConvertAnnotationFontScopesToAppearanceResources(t *testing.T) {
	// 注解外观有自己的资源字典，允许使用与页面同名的字体资源（如 /F1）。
	// 若按资源名复用页面已缓存的字体，外观会误用页面字体并按错误编码解码，
	// 使图章/水印文字（如 "保密资料"）乱码或不可见。
	pageContent := "BT /F1 12 Tf 10 50 Td (A) Tj ET"
	appearance := "BT /F1 8 Tf 10 10 Td (A) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",         // 1
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>", // 2
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R /Annots [8 0 R] >>",                                              // 3
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",                                                                                                                      // 4 页面 /F1
		"<< /Length " + itoa(len(pageContent)) + " >>\nstream\n" + pageContent + "\nendstream",                                                                                        // 5
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /Differences [65 /Z] >> >>",                                                                 // 6 外观 /F1，码 65 映射为 Z
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 6 0 R >> >> /Length " + itoa(len(appearance)) + " >>\nstream\n" + appearance + "\nendstream", // 7
		"<< /Type /Annot /Subtype /Stamp /Rect [0 0 100 100] /F 4 /AP << /N 7 0 R >> >>",                                                                                              // 8
	}
	page := parseConvertedPage(t, objects)
	var got []string
	for _, text := range layerTexts(page.Content().Layer[0]) {
		if len(text.TextCode) > 0 {
			got = append(got, text.TextCode[0].Value)
		}
	}
	for _, value := range got {
		if value == "Z" {
			return
		}
	}
	t.Fatalf("annotation text = %v, want Z (annotation must use its own /F1, not the page /F1)", got)
}

func TestConvertAppliesExtGStateOpacity(t *testing.T) {
	// ExtGState 的 ca/CA 是透明度的来源之一；忽略它会让半透明注解（如整页
	// 灰色背景）变成不透明色块。
	content := "q /GS1 gs 0 0 100 100 re f Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /ExtGState << /GS1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /ExtGState /ca 0.5 >>",
	}
	page := parseConvertedPage(t, objects)
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) == 0 {
		t.Fatal("extgstate path not emitted")
	}
	alpha := paths[0].Alpha
	if alpha == nil {
		t.Fatal("path Alpha is nil, want OFD alpha for ca=0.5")
	}
	// OFD 图元 Alpha 是不透明度：ca=0.5 → 不透明度 0.5 → Alpha 128。
	if *alpha < 126 || *alpha > 130 {
		t.Fatalf("path Alpha = %d, want ~128", *alpha)
	}
}
