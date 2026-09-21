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
	for _, item := range page.Content().Layer[0].PathObject {
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) == 0 {
		t.Fatal("extgstate path not emitted")
	}
	alpha := paths[0].Alpha
	if alpha == nil {
		t.Fatal("path Alpha is nil, want OFD transparency for ca=0.5")
	}
	// OFD 图元 Alpha 是透明度：ca=0.5 → 不透明度 0.5 → 透明度 128。
	if *alpha < 126 || *alpha > 130 {
		t.Fatalf("path Alpha = %d, want ~128", *alpha)
	}
}
