package pdf2ofd

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"math"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func multiplyShadingGroupPDF(opacity float64) []byte {
	content := "0 .8 0 rg 10 10 80 80 re f 0 0 .8 rg 40 40 20 20 re f q /Outer gs /Fm Do Q 1 0 0 rg 92 10 5 5 re f BT /F1 8 Tf 10 95 Td (Label) Tj ET"
	form := "q /Inner gs /Sh sh Q"
	function := "{ pop .5 .5 .5 }"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Fm 5 0 R >> /ExtGState << /Outer 6 0 R >> /Font << /F1 9 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [10 10 90 90] /Group << /S /Transparency /I false /K false >> /Resources << /ExtGState << /Inner 7 0 R >> /Shading << /Sh << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [10 10 90 10] /Function 8 0 R /Extend [true true] >> >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
		fmt.Sprintf("<< /Type /ExtGState /BM /Multiply /ca %g /CA %g >>", opacity, opacity),
		"<< /Type /ExtGState /BM /Normal /ca 1 /CA 1 >>",
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1] /Range [0 1 0 1 0 1] /Length %d >>\nstream\n%s\nendstream", len(function), function),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

func TestMultiplyShadingGroupKeepsBackdropColourAndOuterOpacity(t *testing.T) {
	for _, opacity := range []float64{0, .75, 1} {
		var output bytes.Buffer
		if err := Convert(t.Context(), multiplyShadingGroupPDF(opacity), &output, ""); err != nil {
			t.Fatal(err)
		}
		ofd, err := parser.NewOFD(output.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ofd.Close() }()
		page := ofd.Documents[0].Pages[0]
		if err := page.EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
		if len(layerImages(page.Content().Layer[0])) != 1 {
			t.Fatal("Multiply shading group was not composited")
		}
		if texts := layerTexts(page.Content().Layer[0]); len(texts) != 1 || texts[0].TextCode[0].Value != "Label" {
			t.Fatal("blend conversion lost text")
		}
		document := render.NewDocument(color.White, ofd.Documents[0])
		defer document.Close()
		img, err := document.RasterizePage(page, render.BackendCanvas, geom.DPMM(4))
		if err != nil {
			t.Fatal(err)
		}
		wantChannel := byte(math.Round(204*(1-opacity) + 204*127/255.0*opacity))
		for _, tc := range []struct {
			x, y float64
			want color.RGBA
		}{
			{20, 20, color.RGBA{G: wantChannel, A: 255}},
			{50, 50, color.RGBA{B: wantChannel, A: 255}},
			{5, 20, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
			{94, 12, color.RGBA{R: 255, A: 255}},
		} {
			got := img.RGBAAt(int(tc.x*pdfPointToMillimeter*4), int((100-tc.y)*pdfPointToMillimeter*4))
			if math.Abs(float64(got.R)-float64(tc.want.R)) > 3 || math.Abs(float64(got.G)-float64(tc.want.G)) > 3 || math.Abs(float64(got.B)-float64(tc.want.B)) > 3 {
				t.Fatalf("opacity=%g pixel(%g,%g)=%v want %v", opacity, tc.x, tc.y, got, tc.want)
			}
		}
	}
}

func TestMultiplyShadingGroupStillReportsApproximationAndUsesBudgets(t *testing.T) {
	input := multiplyShadingGroupPDF(.75)
	requireBudgetError(t, input, Limits{MaxImagePixels: 100}, "image_pixels")
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 100}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 1000}, "operations")
	var output bytes.Buffer
	var diagnostics []Diagnostic
	err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	if !errors.Is(err, ErrUnsupported) || output.Len() != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "graphics.blend" {
		t.Fatalf("error=%v output=%d diagnostics=%v", err, output.Len(), diagnostics)
	}
}

func TestVectorAppleKeepsLabelAndFreckleLuminosityMask(t *testing.T) {
	input, err := os.ReadFile("../../testdata/pdfcpu/VectorApple.pdf")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("VectorApple.pdf 文件不存在，跳过样例视觉回归")
	}
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{OnDiagnostic: func(d Diagnostic) {
		if d.Code == "image.decode" {
			t.Errorf("样例图片不应丢失：%+v", d)
		}
	}}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	document := render.NewDocument(color.White, ofd.Documents[0])
	defer document.Close()
	// 144dpi，与源 PDF 的标签取样位置一致。测试颜色类别与文字对比，避开抗锯齿边缘。
	img, err := document.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPI(144))
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range [][2]int{{402, 209}, {420, 210}, {414, 190}} {
		got := img.RGBAAt(pt[0], pt[1])
		if got.G < 70 || got.G <= got.B+25 || got.R <= got.B+15 {
			t.Fatalf("label green pixel %v=%v", pt, got)
		}
	}
	blue, green := 0, 0
	for y := 220; y < 240; y++ {
		for x := 399; x < 433; x++ {
			value := img.RGBAAt(x, y)
			if value.B > value.R+25 && value.B > value.G+25 {
				blue++
			}
			if value.G > value.B+25 && value.R > value.B+15 {
				green++
			}
		}
	}
	if blue < 40 || green < 40 {
		t.Fatalf("label lettering contrast lost: blue=%d green=%d", blue, green)
	}
	// Poppler 26.01.0 在 144dpi 的源 PDF 参考像素；允许插值与像素对齐误差。
	// 丢失 Separation/Black 亮度蒙版时，这些斑点会比参考值亮约 20–50 级。
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{224, 251, color.RGBA{245, 106, 112, 255}},
		{265, 202, color.RGBA{243, 103, 104, 255}},
		{351, 169, color.RGBA{197, 46, 67, 255}},
		{308, 331, color.RGBA{204, 65, 78, 255}},
	} {
		got := img.RGBAAt(tc.x, tc.y)
		if math.Abs(float64(got.R)-float64(tc.want.R)) > 10 || math.Abs(float64(got.G)-float64(tc.want.G)) > 10 || math.Abs(float64(got.B)-float64(tc.want.B)) > 10 {
			t.Fatalf("斑点(%d,%d)=%v，期望接近 %v", tc.x, tc.y, got, tc.want)
		}
	}
}
