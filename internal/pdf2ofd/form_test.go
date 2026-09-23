package pdf2ofd

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func TestConvertFormXObjectAppliesMatrix(t *testing.T) {
	// Form XObject 的 /Matrix 与文本 Tm 必须同时生效。此前从 /Matrix 数组读取的
	// 数值是 pdfcpu 的 types.Integer/Float，anyFloat 只识别 int/float64，导致矩阵
	// 被读成全零，Form 内所有文字堆叠到表单原点（代码块被压成一行）。
	form := "BT /F1 10 Tf 1 0 0 1 0 50 Tm (AB) Tj ET BT /F1 10 Tf 1 0 0 1 20 100 Tm (CD) Tj ET"
	content := "q /Fm Do Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 7 0 R >> /XObject << /Fm 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 300 300] /Matrix [2 0 0 2 10 10] /Resources << /Font << /F1 7 0 R >> >> /Length " + itoa(len(form)) + " >>\nstream\n" + form + "\nendstream",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
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
	positions := map[string][2]float64{}
	for _, item := range layerTexts(page.Content().Layer[0]) {
		value := ""
		for _, code := range item.TextCode {
			value += code.Value
		}
		positions[value] = [2]float64{item.Boundary.X, item.Boundary.Y}
	}
	ab, okAB := positions["AB"]
	cd, okCD := positions["CD"]
	if !okAB || !okCD {
		t.Fatalf("expected both form text objects, got %v", positions)
	}
	if ab == cd {
		t.Fatalf("form text objects overlap at %v; /Matrix not applied", ab)
	}
	// Matrix [2 0 0 2 10 10] + Tm (0,50) → 页面 (10,110)pt → x≈3.528mm。
	if ab[0] < 3 || ab[0] > 4 {
		t.Fatalf("first text X = %g, want ~3.528", ab[0])
	}
	// Tm (20,100) → 页面 (50,210)pt，应位于第一个文字右上方（Y 更小）。
	if cd[0] <= ab[0] || cd[1] >= ab[1] {
		t.Fatalf("second text %v not positioned relative to first %v", cd, ab)
	}
}

func TestConvertFormGroupAlphaKeepsOuterOpacity(t *testing.T) {
	// Form XObject 是透明度组：外层 /ca 0.5 必须保留为组透明度，不能被 Form
	// 内部的重置 gs（ca=1）覆盖。OFD 没有混合模式，这里只验证组透明度。
	form := "q /GS1 gs 20 0 0 20 0 0 cm /Im1 Do Q"
	content := "q /GS0 gs /Fm Do Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Fm 4 0 R >> /ExtGState << /GS0 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Matrix [1 0 0 1 0 0] /Resources << /XObject << /Im1 7 0 R >> /ExtGState << /GS1 8 0 R >> >> /Length " + itoa(len(form)) + " >>\nstream\n" + form + "\nendstream",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 12 >>\nstream\n\xc8\x64\x32\x28\x50\x78\x0a\x14\x1e\x3c\x46\x50\nendstream",
		"<< /Type /ExtGState /ca 1 /CA 1 >>",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
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
	images := layerImages(page.Content().Layer[0])
	if len(images) != 1 {
		t.Fatalf("image objects = %d, want 1", len(images))
	}
	if images[0].Alpha == nil {
		t.Fatal("form group alpha was dropped; expected Alpha ~128")
	}
	if got := *images[0].Alpha; got < 126 || got > 129 {
		t.Fatalf("image Alpha = %d, want ~128", got)
	}
}

func TestConvertBlendModeFormDoesNotApplyGroupAlpha(t *testing.T) {
	// BM 为 Multiply/HardLight 等时，单独套用 ca/CA 会与混合效果叠加出偏差，
	// 因此不按纯透明度近似，保持 Form 内部自身的叠加方式。
	form := "q /GS1 gs 20 0 0 20 0 0 cm /Im1 Do Q"
	content := "q /GS0 gs /Fm Do Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Fm 4 0 R >> /ExtGState << /GS0 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Matrix [1 0 0 1 0 0] /Resources << /XObject << /Im1 7 0 R >> /ExtGState << /GS1 8 0 R >> >> /Length " + itoa(len(form)) + " >>\nstream\n" + form + "\nendstream",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 /BM /Multiply >>",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 12 >>\nstream\n\xc8\x64\x32\x28\x50\x78\x0a\x14\x1e\x3c\x46\x50\nendstream",
		"<< /Type /ExtGState /ca 1 /CA 1 >>",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
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
	images := layerImages(page.Content().Layer[0])
	if len(images) != 1 {
		t.Fatalf("image objects = %d, want 1", len(images))
	}
	if images[0].Alpha != nil {
		t.Fatalf("blend-mode form image Alpha = %d, want none", *images[0].Alpha)
	}
}
