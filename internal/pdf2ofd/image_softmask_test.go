package pdf2ofd

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func softMaskSamples16(values ...uint16) []byte {
	data := make([]byte, len(values)*2)
	for i, value := range values {
		binary.BigEndian.PutUint16(data[i*2:], value)
	}
	return data
}

func TestPDFSoftMaskPreserves16BitColourAndAlpha(t *testing.T) {
	for _, baseBits := range []int{8, 16} {
		for _, maskBits := range []int{8, 16} {
			if baseBits == 8 && maskBits == 8 {
				continue
			}
			for _, invert := range []bool{false, true} {
				t.Run(fmt.Sprintf("base%d_mask%d_invert%v", baseBits, maskBits, invert), func(t *testing.T) {
					maskData := []byte{0x12, 0xbe}
					alphas := []uint16{0x1212, 0xbebe}
					if maskBits == 16 {
						alphas = []uint16{0x1234, 0xbeef}
						maskData = softMaskSamples16(alphas...)
					}
					mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(2), "Height": types.Integer(1), "ColorSpace": types.Name("DeviceGray"), "BitsPerComponent": types.Integer(maskBits)}, Content: maskData}
					if invert {
						mask.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
					}
					baseData := bytes.Repeat([]byte{0x12, 0x34, 0x56}, 4)
					want := color.NRGBA64{R: 0x1212, G: 0x3434, B: 0x5656}
					if baseBits == 16 {
						baseData = bytes.Repeat(softMaskSamples16(0x1234, 0x3456, 0x5678), 4)
						want.R, want.G, want.B = 0x1234, 0x3456, 0x5678
					}
					base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(4), "Height": types.Integer(1), "ColorSpace": types.Name("DeviceRGB"), "BitsPerComponent": types.Integer(baseBits), "SMask": mask}, Content: baseData}
					data, _, err := pdfImageData(nil, base, pdfColor{})
					if err != nil {
						t.Fatal(err)
					}
					decoded, _, err := image.Decode(bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					if _, ok := decoded.(*image.NRGBA64); !ok {
						t.Fatalf("soft mask lost 16-bit channels: %T", decoded)
					}
					for x := 0; x < 4; x++ {
						want.A = alphas[x/2]
						if invert {
							want.A = 65535 - want.A
						}
						got := color.NRGBA64Model.Convert(decoded.At(x, 0)).(color.NRGBA64)
						if got != want {
							t.Fatalf("pixel %d=%#v want %#v", x, got, want)
						}
					}
				})
			}
		}
	}
}

func softMask16ImagePDF(content, extra string, samples []byte, extraObjects ...string) []byte {
	base := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 4 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 16 /SMask 6 0 R /Length 24 >>\nstream\n%s\nendstream", bytes.Repeat(softMaskSamples16(65535, 0, 0), 4))
	mask := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 4 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 16 %s /Length %d >>\nstream\n%s\nendstream", extra, len(samples), samples)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		base, mask,
	}
	return assemblePDF(append(objects, extraObjects...))
}

func TestPDF16BitSoftMaskResolvesIndirectDecodeNumbers(t *testing.T) {
	input := softMask16ImagePDF("/Im0 Do", "/Decode [7 0 R 8 0 R]", softMaskSamples16(65535, 49151, 16384, 0), "1", "0")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	decoded := decodeFirstPageImage(t, output.Bytes())
	for x, alpha := range []byte{0, 64, 191, 255} {
		if got := decoded.NRGBAAt(x, 0).A; got != alpha {
			t.Fatalf("indirect Decode pixel %d alpha=%d want %d", x, got, alpha)
		}
	}
}

func TestPDF16BitSoftMaskStrictConversionRendersPartialTransparency(t *testing.T) {
	content := "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q"
	var output bytes.Buffer
	var diagnostics []Diagnostic
	input := softMask16ImagePDF(content, "", softMaskSamples16(0, 16384, 49151, 65535))
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}); err != nil {
		t.Fatalf("error=%v diagnostics=%v", err, diagnostics)
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
	for i, alpha := range []byte{0, 64, 191, 255} {
		x, y := float64(40+i*40)*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
		got := img.RGBAAt(int(x), int(y))
		if math.Abs(float64(got.R)-float64(alpha)) > 2 || got.G > 2 || math.Abs(float64(got.B)-float64(255-alpha)) > 2 {
			t.Fatalf("block %d=%v want R=%d B=%d", i, got, alpha, 255-alpha)
		}
	}
}

func TestPDF16BitSoftMaskUsesBudgetsAndCache(t *testing.T) {
	samples := softMaskSamples16(0, 16384, 49151, 65535)
	input := softMask16ImagePDF("/Im0 Do", "", samples)
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 11}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 9}, "operations")
	var output bytes.Buffer
	// 基础图、掩码、合成各 4 像素；重复绘制复用合成缓存。
	if err := ConvertWithOptions(t.Context(), softMask16ImagePDF("/Im0 Do /Im0 Do", "", samples), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 12}}); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(samples); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	input = softMask16ImagePDF("/Im0 Do", "/Filter /FlateDecode", compressed.Bytes())
	requireBudgetError(t, input, Limits{MaxTotalDecodedBytes: 38}, "total_decoded_bytes")
	output.Reset()
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
}

func TestPDF16BitGraySoftMaskPreservesColourAndDecodeRange(t *testing.T) {
	mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(2), "Height": types.Integer(1), "BitsPerComponent": types.Integer(16), "Decode": types.Array{types.Float(.25), types.Float(.75)}}, Content: softMaskSamples16(0, 65535)}
	base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(2), "Height": types.Integer(1), "BitsPerComponent": types.Integer(16), "ColorSpace": types.Name("DeviceGray"), "SMask": mask}, Content: softMaskSamples16(0x1234, 0xbeef)}
	data, _, err := pdfImageData(nil, base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for x, want := range []color.NRGBA64{{R: 0x1234, G: 0x1234, B: 0x1234, A: 16384}, {R: 0xbeef, G: 0xbeef, B: 0xbeef, A: 49151}} {
		if got := color.NRGBA64Model.Convert(decoded.At(x, 0)).(color.NRGBA64); got != want {
			t.Fatalf("gray pixel %d=%#v want %#v", x, got, want)
		}
	}
}

type cancelSoftMaskImage struct {
	image.Image
	cancel context.CancelFunc
	reads  int
}

func (s *cancelSoftMaskImage) At(x, y int) color.Color {
	s.reads++
	s.cancel()
	return s.Image.At(x, y)
}

func TestPDF16BitSoftMaskStopsCompositingWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{}).Normalize()
	control := &conversionControl{ctx: ctx, options: options}
	base := &cancelSoftMaskImage{Image: image.NewGray16(image.Rect(0, 0, 4, 4)), cancel: cancel}
	mask := &types.StreamDict{Content: softMaskSamples16(32768)}
	_, ok := applyPDFImageSoftMask16(nil, base, mask, 1, 1, 16, control)
	if ok || control.check() != context.Canceled || base.reads != 4 {
		t.Fatalf("ok=%v error=%v pixels read=%d", ok, control.check(), base.reads)
	}
}

func TestPDFTruncated16BitSoftMaskReportsAndKeepsBase(t *testing.T) {
	input := softMask16ImagePDF("/Im0 Do", "", softMaskSamples16(0, 65535))
	for _, strict := range []bool{false, true} {
		var output bytes.Buffer
		var diagnostics []Diagnostic
		err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask" || diagnostics[0].Object != 5 {
			t.Fatalf("diagnostics=%v", diagnostics)
		}
		if strict {
			if !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
				t.Fatalf("strict error=%v output=%d", err, output.Len())
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		decoded := decodeFirstPageImage(t, output.Bytes())
		if value := decoded.NRGBAAt(0, 0); value.R != 255 || value.A != 255 {
			t.Fatalf("broken mask lost base: %v", value)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel error=%v output=%d", err, output.Len())
	}
}
