package pdf2ofd

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func TestConvertMultiplyFillKeepsSourceOnTransparentBackdrop(t *testing.T) {
	// Without an existing backdrop Multiply retains the original source color
	// and coverage. A page with no painted backdrop must not introduce black.
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
	if images := layerImages(page.Content().Layer[0]); len(images) != 1 {
		t.Fatalf("composited images=%d", len(images))
	}
	img := decodeFirstPageImage(t, output.Bytes())
	c := img.NRGBAAt(img.Bounds().Dx()/2, img.Bounds().Dy()/2)
	if absByteDifference(c.R, 251) > 2 || absByteDifference(c.G, 92) > 2 || absByteDifference(c.B, 137) > 2 || c.A != 255 {
		t.Fatalf("Multiply on empty backdrop=%v", c)
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
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	if paths[0].Alpha != nil {
		t.Fatalf("normal fill should stay opaque, Alpha = %d", *paths[0].Alpha)
	}
}
