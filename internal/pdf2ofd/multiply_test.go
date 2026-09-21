package pdf2ofd

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func TestConvertMultiplyFillBecomesTranslucent(t *testing.T) {
	// Multiply 混合的纯色填充（如高亮注释）应近似为半透明，使下方文字透出：
	// 不透明度 a = (255-minC)/255，修正色 C′ = 255 - (255-C)/a。
	content := []byte("q /GS gs 0.984314 0.360784 0.537255 rg 0 0 100 100 re f Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /ExtGState << /GS 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /ExtGState /BM /Multiply >>",
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	path := paths[0]
	if path.Alpha == nil {
		t.Fatal("multiply fill should be translucent; Alpha is nil")
	}
	// minC = 92 → a = 163/255 ≈ 0.639 → 透明度 (1-a)*255 ≈ 92。
	if got := *path.Alpha; got < 88 || got > 96 {
		t.Fatalf("Alpha = %d, want ~92", got)
	}
	color := path.FillColor
	if color == nil || color.Value == nil {
		t.Fatal("multiply fill has no solid color")
	}
	if color.Value.R < 240 || color.Value.G > 7 || color.Value.B < 62 || color.Value.B > 80 {
		t.Fatalf("corrected color = %v, want ~(249,0,70)", color.Value.RGBA)
	}
}

func TestConvertNormalFillStaysOpaque(t *testing.T) {
	content := []byte("q 0.984314 0.360784 0.537255 rg 0 0 100 100 re f Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	if paths[0].Alpha != nil {
		t.Fatalf("normal fill should stay opaque, Alpha = %d", *paths[0].Alpha)
	}
}
