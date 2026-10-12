package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func conflictingJPXMaskPDF(t *testing.T, content string, alpha byte) []byte {
	t.Helper()
	data := jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 65535, A: 32769}).Raw
	return budgetImagePDF(content,
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /SMaskInData 1 /SMask 6 0 R /Mask 7 0 R /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n%s\nendstream", []byte{alpha}),
		maskImage(1024, 1024, "/Filter /FlateDecode", []byte("ignored corrupt explicit mask")))
}

func TestPDFJPXConflictingSoftMaskReplacesAlphaWithoutMultiplying(t *testing.T) {
	for _, mode := range []int{1, 2} {
		for _, alpha := range []byte{0, 64, 255} {
			pixel := color.NRGBA64{R: 65535, A: 32769}
			if mode == 2 {
				pixel.R = pixel.A
			}
			base := jpxAlphaStream(t, 16, mode, pixel)
			base.Dict["SMask"] = &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray")}, Content: []byte{alpha}}
			var diagnostics []Diagnostic
			opts, _ := (Options{OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}, source: Diagnostic{Page: 1, Object: 5, Resource: "Im0"}}
			data, _, err := p.imageData(base, pdfColor{})
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: 65535, A: uint16(alpha) * 257}) {
				t.Fatalf("mode%d alpha%d=%#v", mode, alpha, got)
			}
			if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_priority" || diagnostics[0].Object != 5 {
				t.Fatalf("diagnostics=%v", diagnostics)
			}
		}
	}
}

func TestPDFJPXConflictingSoftMaskCachesCompatibilityAndChargesSelectedMask(t *testing.T) {
	input := conflictingJPXMaskPDF(t, "/Im0 Do /Im0 Do", 64)
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 128}, "total_image_pixels")
	var diagnostics []Diagnostic
	var output bytes.Buffer
	err := ConvertWithOptions(t.Context(), input, &output, "", Options{Limits: Limits{MaxImagePixels: 64, MaxTotalImagePixels: 129}, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_priority" {
		t.Fatalf("cached diagnostics=%v", diagnostics)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got != (color.NRGBA{R: 255, A: 64}) {
		t.Fatalf("cached replacement alpha=%v", got)
	}
}

func TestPDFJPXConflictingSoftMaskStrictAndCancellationKeepOutputEmpty(t *testing.T) {
	input := conflictingJPXMaskPDF(t, "/Im0 Do", 0)
	var output bytes.Buffer
	err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true})
	if !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
		t.Fatalf("strict err=%v output=%d", err, output.Len())
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err = ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel err=%v output=%d", err, output.Len())
	}
}

func TestPDFJPXConflictingBlackSoftMaskMatchesSourceBlueBackground(t *testing.T) {
	var output bytes.Buffer
	input := conflictingJPXMaskPDF(t, "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", 0)
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	doc := render.NewDocument(color.White, ofd.Documents[0])
	defer doc.Close()
	img, err := doc.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPMM(4))
	if err != nil {
		t.Fatal(err)
	}
	x, y := 100*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
	if got := img.RGBAAt(int(x), int(y)); got != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("source blue became dark purple: %v", got)
	}
}

func TestPDFJPXBrokenConflictingSoftMaskKeepsEmbeddedOpacityWithDiagnostic(t *testing.T) {
	for _, mask := range []types.Object{types.Name("Invalid"), &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray")}, Raw: []byte("broken flate"), FilterPipeline: []types.PDFFilter{{Name: "FlateDecode"}}}} {
		base := jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 65535, A: 32769})
		base.Dict["SMask"] = mask
		var diagnostics []Diagnostic
		opts, _ := (Options{OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
		data, _, err := p.imageData(base, pdfColor{})
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: 65535, A: 32769}) {
			t.Fatalf("broken external mask lost embedded image: %#v", got)
		}
		if len(diagnostics) == 0 {
			t.Fatal("broken external mask silently ignored")
		}
	}
}
