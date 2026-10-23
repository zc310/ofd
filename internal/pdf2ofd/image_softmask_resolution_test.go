package pdf2ofd

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func resolutionMask(width, height, bits int, data []byte) *types.StreamDict {
	return &types.StreamDict{Dict: types.Dict{"Width": types.Integer(width), "Height": types.Integer(height), "BitsPerComponent": types.Integer(bits), "ColorSpace": types.Name("DeviceGray")}, Content: data}
}

func resolutionBase(width, height, bits int, data []byte, mask *types.StreamDict) *types.StreamDict {
	return &types.StreamDict{Dict: types.Dict{"Width": types.Integer(width), "Height": types.Integer(height), "BitsPerComponent": types.Integer(bits), "ColorSpace": types.Name("DeviceRGB"), "SMask": mask}, Content: data}
}

func TestPDFSoftMaskKeepsHigherResolutionAnd16BitAdjacentAlpha(t *testing.T) {
	for _, baseBits := range []int{8, 16} {
		for _, maskBits := range []int{4, 8, 16} {
			t.Run(fmt.Sprintf("base%d_mask%d", baseBits, maskBits), func(t *testing.T) {
				alphas := []uint16{0, 16448, 32896, 65535, 65535, 32896, 16448, 0}
				mask := resolutionMask(4, 2, 8, []byte{0, 64, 128, 255, 255, 128, 64, 0})
				if maskBits == 4 {
					mask = resolutionMask(4, 2, 4, []byte{0x05, 0xaf, 0xfa, 0x50})
					alphas = []uint16{0, 21845, 43690, 65535, 65535, 43690, 21845, 0}
				}
				if maskBits == 16 {
					alphas = []uint16{0, 32768, 32769, 65535, 65535, 32769, 32768, 0}
					mask = resolutionMask(4, 2, 16, softMaskSamples16(alphas...))
				}
				pixel := color.NRGBA64{R: 0x1212, G: 0x3434, B: 0x5656, A: 65535}
				base := resolutionBase(1, 1, 8, []byte{0x12, 0x34, 0x56}, mask)
				if baseBits == 16 {
					pixel.R, pixel.G, pixel.B = 0x1234, 0x3456, 0x5678
					base = resolutionBase(1, 1, 16, softMaskSamples16(pixel.R, pixel.G, pixel.B), mask)
				}
				options, _ := (Options{Strict: true}).Normalize()
				p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
				img := jpxDecodedImage(t, &p, base)
				if img.Bounds() != image.Rect(0, 0, 4, 2) {
					t.Fatalf("mask detail lost: %v", img.Bounds())
				}
				for i, a := range alphas {
					pixel.A = a
					got := color.NRGBA64Model.Convert(img.At(i%4, i/4)).(color.NRGBA64)
					if source, ok := img.(*image.NRGBA); ok {
						v := source.NRGBAAt(i%4, i/4)
						got = color.NRGBA64{R: uint16(v.R) * 257, G: uint16(v.G) * 257, B: uint16(v.B) * 257, A: uint16(v.A) * 257}
					}
					if got != pixel {
						t.Fatalf("pixel %d=%#v want %#v", i, got, pixel)
					}
				}
			})
		}
	}
}

func TestPDFSoftMaskKeepsBothAxesWhenResolutionsCross(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		baseWidth, baseHeight, maskWidth, maskHeight := 4, 1, 1, 4
		if vertical {
			baseWidth, baseHeight, maskWidth, maskHeight = 1, 4, 4, 1
		}
		colors := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0}
		mask := resolutionMask(maskWidth, maskHeight, 8, []byte{0, 64, 128, 255})
		img := jpxDecodedImage(t, &pdfInterpreter{}, resolutionBase(baseWidth, baseHeight, 8, colors, mask))
		if img.Bounds() != image.Rect(0, 0, 4, 4) {
			t.Fatalf("crossed axes collapsed: %v", img.Bounds())
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				c, a := x, y
				if vertical {
					c, a = y, x
				}
				want := color.NRGBA{R: colors[c*3], G: colors[c*3+1], B: colors[c*3+2], A: []byte{0, 64, 128, 255}[a]}
				if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got != want {
					t.Fatalf("vertical=%v pixel %d,%d=%v want %v", vertical, x, y, got, want)
				}
			}
		}
	}
}

func TestPDFSoftMaskExpanded16BitBaseUsesNonzeroSourceOrigin(t *testing.T) {
	base := image.NewNRGBA64(image.Rect(-2, 3, 0, 4))
	base.SetNRGBA64(-2, 3, color.NRGBA64{R: 0x1234, G: 0x3456, B: 0x5678, A: 1})
	base.SetNRGBA64(-1, 3, color.NRGBA64{R: 0x789a, G: 0x9abc, B: 0xbcde, A: 2})
	mask := resolutionMask(4, 2, 16, softMaskSamples16(0, 32768, 32769, 65535, 65535, 32769, 32768, 0))
	data, ok := applyPDFImageSoftMask16(nil, base, mask, 4, 2, 16, nil)
	if !ok {
		t.Fatal("composition failed")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			want := base.NRGBA64At(-2+x/2, 3)
			want.A = []uint16{0, 32768, 32769, 65535, 65535, 32769, 32768, 0}[y*4+x]
			if got := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64); got != want {
				t.Fatalf("pixel %d,%d=%#v want %#v", x, y, got, want)
			}
		}
	}
}

func TestPDFSoftMaskKeepsCompressedMaskResolution(t *testing.T) {
	for _, kind := range []string{"Flate", "JPEG", "JPX16", "JBIG2"} {
		for _, baseBits := range []int{8, 16} {
			t.Run(fmt.Sprintf("%s_base%d", kind, baseBits), func(t *testing.T) {
				mask := resolutionMask(4, 2, 8, []byte{0, 64, 128, 255, 255, 128, 64, 0})
				want := []uint16{0, 16448, 32896, 65535, 65535, 32896, 16448, 0}
				switch kind {
				case "Flate":
					var compressed bytes.Buffer
					w := zlib.NewWriter(&compressed)
					if _, err := w.Write(mask.Content); err != nil {
						t.Fatal(err)
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					mask.Raw, mask.Content = compressed.Bytes(), nil
					mask.FilterPipeline = []types.PDFFilter{{Name: "FlateDecode"}}
				case "JPEG":
					gray := image.NewGray(image.Rect(0, 0, 4, 2))
					copy(gray.Pix, mask.Content)
					var compressed bytes.Buffer
					if err := jpeg.Encode(&compressed, gray, &jpeg.Options{Quality: 100}); err != nil {
						t.Fatal(err)
					}
					decoded, err := jpeg.Decode(bytes.NewReader(compressed.Bytes()))
					if err != nil {
						t.Fatal(err)
					}
					for i := range want {
						want[i] = uint16(color.GrayModel.Convert(decoded.At(i%4, i/4)).(color.Gray).Y) * 257
					}
					mask.Raw, mask.Content = compressed.Bytes(), nil
					mask.FilterPipeline = []types.PDFFilter{{Name: "DCTDecode"}}
				case "JPX16":
					gray := image.NewGray16(image.Rect(0, 0, 4, 2))
					want = []uint16{0, 32768, 32769, 65535, 65535, 32769, 32768, 0}
					for i, a := range want {
						gray.SetGray16(i%4, i/4, color.Gray16{Y: a})
					}
					mask.Raw, mask.Content = encodeJPX(t, gray, jpeg2000.FormatJP2), nil
					mask.FilterPipeline = []types.PDFFilter{{Name: "JPXDecode"}}
					// JPX 必须按实际 4x2 码流合成，忽略错误字典尺寸和位深。
					mask.Dict["Width"], mask.Dict["Height"], mask.Dict["BitsPerComponent"] = types.Integer(1), types.Integer(1), types.Integer(8)
				case "JBIG2":
					mask.Raw, mask.Content = matteJBIG2Page(4, 2, false), nil
					mask.FilterPipeline = []types.PDFFilter{{Name: "JBIG2Decode"}}
					mask.Dict["BitsPerComponent"] = types.Integer(1)
					for i := range want {
						want[i] = 65535
					}
				}
				options, _ := (Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 17}}).Normalize()
				p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
				base := resolutionBase(1, 1, 16, softMaskSamples16(0x1234, 0x3456, 0x5678), mask)
				pixel := color.NRGBA64{R: 0x1234, G: 0x3456, B: 0x5678, A: 65535}
				if baseBits == 8 {
					base = resolutionBase(1, 1, 8, []byte{0x12, 0x34, 0x56}, mask)
					pixel.R, pixel.G, pixel.B = 0x1212, 0x3434, 0x5656
				}
				img := jpxDecodedImage(t, &p, base)
				if img.Bounds() != image.Rect(0, 0, 4, 2) {
					t.Fatalf("%s mask downsampled: %v", kind, img.Bounds())
				}
				for i, a := range want {
					pixel.A = a
					got := color.NRGBA64Model.Convert(img.At(i%4, i/4)).(color.NRGBA64)
					if source, ok := img.(*image.NRGBA); ok {
						v := source.NRGBAAt(i%4, i/4)
						got = color.NRGBA64{R: uint16(v.R) * 257, G: uint16(v.G) * 257, B: uint16(v.B) * 257, A: uint16(v.A) * 257}
					}
					if got != pixel {
						t.Fatalf("pixel %d=%#v want %#v", i, got, pixel)
					}
				}
			})
		}
	}
}

func resolutionMaskPDF(content string) []byte {
	base := budgetImage(1, 1, "/SMask 6 0 R", []byte{255, 0, 0})
	mask := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 4 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 8 >>\nstream\n%s\nendstream", []byte{0, 64, 128, 255, 255, 128, 64, 0})
	return budgetImagePDF(content, base, mask)
}

func TestPDFExpandedSoftMaskBudgetsAndCachedComposition(t *testing.T) {
	input := resolutionMaskPDF("/Im0 Do")
	// 底图 1、掩码 8、扩展合成 8；重复绘制缓存合成结果。
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 16}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 16}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), resolutionMaskPDF("/Im0 Do /Im0 Do"), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 17}}); err != nil {
		t.Fatal(err)
	}
	if img := decodeFirstPageImage(t, output.Bytes()); img.Bounds() != image.Rect(0, 0, 4, 2) {
		t.Fatalf("cached mask dimensions=%v", img.Bounds())
	}
	// 4x1 与 1x4 的输入各自只有 4 像素，合成后的 4x4 也要受单图上限约束。
	base := budgetImage(4, 1, "/SMask 6 0 R", bytes.Repeat([]byte{255, 0, 0}, 4))
	mask := "<< /Type /XObject /Subtype /Image /Width 1 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 4 >>\nstream\n\x00\x40\x80\xff\nendstream"
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, mask), Limits{MaxImagePixels: 4}, "image_pixels")
}

func TestPDFExpandedSoftMaskRetainsGraphicsStateMaskDetails(t *testing.T) {
	mask := []byte{0, 64, 128, 255, 255, 128, 64, 0}
	input := maskedImagePDF(string(mask), 4, 2, "9 0 R")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	img := decodeFirstPageImage(t, output.Bytes())
	if img.Bounds() != image.Rect(0, 0, 4, 2) {
		t.Fatalf("graphics mask detail lost: %v", img.Bounds())
	}
	for i, a := range mask {
		if got := img.NRGBAAt(i%4, i/4).A; got != a {
			t.Fatalf("graphics mask pixel %d alpha=%d want %d", i, got, a)
		}
	}
}

func TestPDFExpandedSoftMaskReturnsCancellationDuringResampling(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{}).Normalize()
	control := &conversionControl{ctx: ctx, options: options}
	base := &cancelSoftMaskImage{Image: image.NewGray16(image.Rect(0, 0, 1, 1)), cancel: cancel}
	mask := resolutionMask(4, 2, 16, softMaskSamples16(0, 32768, 32769, 65535, 65535, 32769, 32768, 0))
	if _, ok := applyPDFImageSoftMask16(nil, base, mask, 4, 2, 16, control); ok || control.check() != context.Canceled || base.reads != 4 {
		t.Fatalf("ok=%v err=%v reads=%d", ok, control.check(), base.reads)
	}
}

func TestPDFExpandedSoftMaskStrictConversionRendersAllTransparencyLevels(t *testing.T) {
	var output bytes.Buffer
	input := resolutionMaskPDF("0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q")
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
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			a := []int{0, 64, 128, 255, 255, 128, 64, 0}[y*4+x]
			px, py := float64(40+x*40)*pdfPointToMillimeter*4, float64(80+y*40)*pdfPointToMillimeter*4
			got := img.RGBAAt(int(px), int(py))
			want := color.RGBA{R: byte(a), B: byte(255 - a), A: 255}
			for c, v := range []byte{got.R, got.G, got.B} {
				w := []byte{want.R, want.G, want.B}[c]
				if d := int(v) - int(w); d < -2 || d > 2 {
					t.Fatalf("block %d,%d=%v want %v", x, y, got, want)
				}
			}
		}
	}
}

func TestPDFInterpolateUpscalesTinyImageWithSoftMask(t *testing.T) {
	input := bytes.Replace(
		resolutionMaskPDF("/Im0 Do"),
		[]byte("/SMask 6 0 R"),
		[]byte("/SMask 6 0 R /Interpolate true"),
		1,
	)
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	img := decodeFirstPageImage(t, output.Bytes())
	if bounds := img.Bounds(); bounds.Dx()*bounds.Dy() <= 64 {
		t.Fatalf("interpolated image stayed tiny: %v", bounds)
	}
	alpha := img.NRGBAAt(img.Bounds().Min.X+2, img.Bounds().Min.Y+2).A
	if alpha == 0 || alpha == 64 || alpha == 128 || alpha == 255 {
		t.Fatalf("interpolated alpha=%d, want a blended value", alpha)
	}
}
