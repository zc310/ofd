package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPDFMatteRemovesPreblendedColourBeforeApplyingAlpha(t *testing.T) {
	for _, tc := range []struct {
		name, cs                      string
		bpc                           int
		base, mask                    []byte
		matte, baseDecode, maskDecode types.Array
		want                          color.NRGBA64
	}{
		{"white RGB matte", "DeviceRGB", 8, []byte{255, 127, 127}, []byte{128}, types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}, nil, nil, color.NRGBA64{R: 65535, A: 32896}},
		{"black RGB matte", "DeviceRGB", 8, []byte{128, 0, 0}, []byte{128}, types.Array{types.Integer(0), types.Integer(0), types.Integer(0)}, nil, nil, color.NRGBA64{R: 65535, A: 32896}},
		{"base Decode first", "DeviceRGB", 8, []byte{0, 128, 128}, []byte{128}, types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}, types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}, nil, color.NRGBA64{R: 65535, A: 32896}},
		{"mask Decode first", "DeviceRGB", 8, []byte{255, 127, 127}, []byte{127}, types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}, nil, types.Array{types.Integer(1), types.Integer(0)}, color.NRGBA64{R: 65535, A: 32896}},
		{"gray matte", "DeviceGray", 8, []byte{128}, []byte{128}, types.Array{types.Integer(0)}, nil, nil, color.NRGBA64{R: 65535, G: 65535, B: 65535, A: 32896}},
		{"packed gray matte", "DeviceGray", 4, []byte{0x80}, []byte{0x80}, types.Array{types.Integer(0)}, nil, nil, color.NRGBA64{R: 65535, G: 65535, B: 65535, A: 34952}},
		{"16 bit matte", "DeviceRGB", 16, softMaskSamples16(65535, 32767, 32767), softMaskSamples16(32768), types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}, nil, nil, color.NRGBA64{R: 65535, A: 32768}},
		{"zero alpha", "DeviceRGB", 16, softMaskSamples16(65535, 65535, 65535), softMaskSamples16(0), types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}, nil, nil, color.NRGBA64{}},
		{"opaque alpha", "DeviceRGB", 16, softMaskSamples16(0x1234, 0x3456, 0x5678), softMaskSamples16(65535), types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}, nil, nil, color.NRGBA64{R: 0x1234, G: 0x3456, B: 0x5678, A: 65535}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "ColorSpace": types.Name("DeviceGray"), "BitsPerComponent": types.Integer(tc.bpc), "Matte": tc.matte}, Content: tc.mask}
			if tc.maskDecode != nil {
				mask.Dict["Decode"] = tc.maskDecode
			}
			base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "ColorSpace": types.Name(tc.cs), "BitsPerComponent": types.Integer(tc.bpc), "SMask": mask}, Content: tc.base}
			if tc.baseDecode != nil {
				base.Dict["Decode"] = tc.baseDecode
			}
			data, _, err := pdfImageData(nil, base, pdfColor{})
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if got := color.NRGBA64Model.Convert(img.At(0, 0)).(color.NRGBA64); got != tc.want {
				t.Fatalf("pixel=%#v want %#v", got, tc.want)
			}
		})
	}
}

func TestPDFMatteUnblendsCMYKBeforeRGBConversion(t *testing.T) {
	mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray"), "Matte": types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}}, Content: []byte{128}}
	base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceCMYK"), "SMask": mask}, Content: []byte{128, 0, 0, 0}}
	data, _, err := pdfImageData(nil, base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	gotImage, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// 应与原颜色空间中的纯青色一致；在 RGB 中去底色不满足这个等式。
	base.Dict["SMask"] = nil
	base.Content = []byte{255, 0, 0, 0}
	reference, _, err := pdfImageData(nil, base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	wantImage, _, err := image.Decode(bytes.NewReader(reference))
	if err != nil {
		t.Fatal(err)
	}
	want := color.NRGBAModel.Convert(wantImage.At(0, 0)).(color.NRGBA)
	want.A = 128
	if got := color.NRGBAModel.Convert(gotImage.At(0, 0)).(color.NRGBA); got != want {
		t.Fatalf("CMYK pixel=%v want %v", got, want)
	}
}

func matteImagePDF(content, matte string, extraObjects ...string) []byte {
	mask := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Matte %s /Length 2 >>\nstream\n%s\nendstream", matte, []byte{128, 255})
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		budgetImage(2, 1, "/SMask 6 0 R", []byte{255, 127, 127, 255, 0, 0}), mask,
	}
	return assemblePDF(append(objects, extraObjects...))
}

func TestPDFMatteStrictConversionRendersWithoutWhiteFringe(t *testing.T) {
	var output bytes.Buffer
	input := matteImagePDF("0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", "7 0 R", "[8 0 R 8 0 R 8 0 R]", "1")
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	document := render.NewDocument(color.White, ofd.Documents[0])
	defer document.Close()
	img, err := document.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPMM(4))
	if err != nil {
		t.Fatal(err)
	}
	x, y := 60*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
	got := img.RGBAAt(int(x), int(y))
	if got.R < 126 || got.R > 130 || got.G > 2 || got.B < 125 || got.B > 129 {
		t.Fatalf("white fringe remains: %v", got)
	}
}

func TestPDFMatteSampleKeepsUninterpolatedColourBands(t *testing.T) {
	input, err := os.ReadFile("../../testdata/pdf/pdf-matte-sample.pdf")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("test fixture is not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	document := render.NewDocument(color.White, ofd.Documents[0])
	defer document.Close()
	raster, err := document.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPI(144))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{150, 280, color.RGBA{B: 255, A: 255}},
		{300, 280, color.RGBA{R: 64, B: 191, A: 255}},
		{500, 280, color.RGBA{R: 192, B: 63, A: 255}},
		{650, 280, color.RGBA{R: 255, A: 255}},
		{300, 730, color.RGBA{G: 43, B: 251, A: 255}},
	} {
		got := raster.RGBAAt(tc.x, tc.y)
		if colorDistance(got, tc.want) > 3 {
			t.Errorf("pixel(%d,%d)=%v want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func colorDistance(a, b color.RGBA) uint8 {
	max := uint8(0)
	for _, d := range []int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
		if d < 0 {
			d = -d
		}
		if uint8(d) > max {
			max = uint8(d)
		}
	}
	return max
}

func TestPDFInvalidMatteReportsAndKeepsBaseImage(t *testing.T) {
	// 部分非法结构会被 pdfcpu 先拒绝；直接验证解释器的回退语义。
	for _, invalid := range []types.Object{
		types.Array{types.Integer(1)},
		types.Array{types.Integer(1), types.Integer(1), types.Integer(2)},
		types.Array{types.Integer(1), types.Integer(1), types.Name("Invalid")},
		types.Name("Invalid"),
	} {
		for _, strict := range []bool{false, true} {
			var diagnostics []Diagnostic
			opts, _ := (Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}, source: Diagnostic{Page: 1, Object: 5, Resource: "Im0"}}
			mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray"), "Matte": invalid}, Content: []byte{128}}
			base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceRGB"), "SMask": mask}, Content: []byte{255, 127, 127}}
			data, _, err := p.imageData(base, pdfColor{})
			if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_matte" || diagnostics[0].Object != 5 {
				t.Fatalf("diagnostics=%v", diagnostics)
			}
			if strict {
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("strict error=%v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				img, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got != (color.NRGBA{R: 255, G: 127, B: 127, A: 255}) {
					t.Fatalf("base changed: %v", got)
				}
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, matteImagePDF("/Im0 Do", "[1]"), &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel error=%v output=%d", err, output.Len())
	}
}

func TestPDFMatteUsesProcessingAndImageBudgetsWithCachedComposition(t *testing.T) {
	input := matteImagePDF("/Im0 Do", "[1 1 1]")
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 5}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 10}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), matteImagePDF("/Im0 Do /Im0 Do", "[1 1 1]"), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 6, MaxOperations: 14}}); err != nil {
		t.Fatal(err)
	}
}

func TestPDFMatteReportsUnsupportedSamplesAndMismatchedDimensions(t *testing.T) {
	for _, kind := range []string{"dimensions", "compressed", "colourspace"} {
		var diagnostics []Diagnostic
		opts, _ := (Options{OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
		mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray"), "Matte": types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}}, Content: []byte{128}}
		base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceRGB"), "SMask": mask}, Content: []byte{255, 127, 127}}
		switch kind {
		case "dimensions":
			mask.Dict["Width"] = types.Integer(2)
		case "compressed":
			mask.FilterPipeline = []types.PDFFilter{{Name: "DCTDecode"}}
		case "colourspace":
			base.Dict["ColorSpace"] = types.Array{types.Name("CalRGB"), types.Dict{}}
		}
		data, format, err := pdfImageDataRaw(nil, base, pdfColor{})
		if err != nil {
			t.Fatal(err)
		}
		// 已编码底图不能掩盖原采样不受支持的事实；须报告并保留原字节。
		result, resultFormat := p.imageMaskData(base, data, format)
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_matte" || !bytes.Equal(result, data) || resultFormat != format {
			t.Fatalf("%s diagnostics=%v base changed=%v", kind, diagnostics, !bytes.Equal(result, data))
		}
	}
}
