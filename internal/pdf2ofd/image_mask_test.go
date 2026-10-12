package pdf2ofd

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPDFColorKeyMatchesOriginalSamples(t *testing.T) {
	for _, tc := range []struct {
		name               string
		width, height, bpc int
		cs                 types.Object
		decode             types.Array
		ranges             types.Array
		raw, alpha         []byte
	}{
		{"RGB inclusive all components", 3, 1, 8, types.Name("DeviceRGB"), nil, types.Array{types.Integer(10), types.Integer(11), types.Integer(20), types.Integer(21), types.Integer(30), types.Integer(31)}, []byte{10, 20, 30, 11, 21, 31, 10, 22, 30}, []byte{0, 0, 255}},
		{"before Decode inversion", 2, 1, 8, types.Name("DeviceGray"), types.Array{types.Integer(1), types.Integer(0)}, types.Array{types.Integer(0), types.Integer(0)}, []byte{0, 255}, []byte{0, 255}},
		{"packed row padding", 3, 2, 2, types.Name("DeviceGray"), nil, types.Array{types.Integer(1), types.Integer(2)}, []byte{0x1b, 0xe4}, []byte{255, 0, 0, 255, 0, 0}},
		{"Indexed index rather than RGB", 2, 1, 4, types.Array{types.Name("Indexed"), types.Name("DeviceRGB"), types.Integer(1), types.HexLiteral("ff0000ff0000")}, nil, types.Array{types.Integer(1), types.Integer(1)}, []byte{0x01}, []byte{255, 0}},
		{"CMYK before RGB conversion", 2, 1, 8, types.Name("DeviceCMYK"), nil, types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(255), types.Integer(255)}, []byte{0, 0, 0, 255, 0, 0, 0, 0}, []byte{0, 255}},
		{"16 bit exact match", 2, 1, 16, types.Name("DeviceGray"), nil, types.Array{types.Integer(0x1234), types.Integer(0x1234)}, []byte{0x12, 0x34, 0x12, 0x35}, []byte{0, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(tc.width), "Height": types.Integer(tc.height), "BitsPerComponent": types.Integer(tc.bpc), "ColorSpace": tc.cs, "Mask": tc.ranges}, Content: tc.raw}
			if tc.decode != nil {
				stream.Dict["Decode"] = tc.decode
			}
			data, format, err := pdfImageData(nil, stream, pdfColor{})
			if err != nil || format != "PNG" {
				t.Fatalf("format=%s error=%v", format, err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			for i, alpha := range tc.alpha {
				_, _, _, got := decoded.At(i%tc.width, i/tc.width).RGBA()
				if got != uint32(alpha)*257 {
					t.Fatalf("pixel %d alpha=%d want %d", i, got, alpha)
				}
			}
			if tc.bpc == 16 {
				r, g, b, a := decoded.At(1, 0).RGBA()
				if r != 0x1235 || g != r || b != r || a != 65535 {
					t.Fatalf("16 bit color lost: %x %x %x %x", r, g, b, a)
				}
			}
		})
	}
}

func TestPDFExplicitMaskUsesStencilPolarityAndResampling(t *testing.T) {
	for _, invert := range []bool{false, true} {
		mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(2), "Height": types.Integer(2), "ImageMask": types.Boolean(true), "BitsPerComponent": types.Integer(1)}, Content: []byte{0x40, 0x80}}
		if invert {
			mask.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
		}
		stream := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(4), "Height": types.Integer(4), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceRGB"), "Mask": mask}, Content: bytes.Repeat([]byte{240, 40, 20}, 16)}
		data, _, err := pdfImageData(nil, stream, pdfColor{})
		if err != nil {
			t.Fatal(err)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				visible := (x/2 == y/2) != invert
				value := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
				if (value.A == 255) != visible || (visible && (value.R != 240 || value.G != 40 || value.B != 20)) {
					t.Fatalf("invert=%v pixel(%d,%d)=%v", invert, x, y, value)
				}
			}
		}
	}
}

func maskImage(width, height int, extra string, data []byte) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ImageMask true /BitsPerComponent 1 %s /Length %d >>\nstream\n%s\nendstream", width, height, extra, len(data), data)
}

func TestPDFMaskStrictConversionRendersTransparentBackground(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		base := budgetImage(2, 1, "/Mask [255 255 0 0 0 0]", []byte{255, 0, 0, 0, 255, 0})
		images := []string{base}
		if explicit {
			images = []string{budgetImage(2, 1, "/Mask 6 0 R", []byte{255, 0, 0, 0, 255, 0}), maskImage(2, 1, "/Decode [1 0]", []byte{0x40})}
		}
		// 蓝底上的红色像素被遮罩移除，右半部分绿色像素保留。
		content := "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q"
		var output bytes.Buffer
		var diagnostics []Diagnostic
		if err := ConvertWithOptions(t.Context(), budgetImagePDF(content, images...), &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}); err != nil {
			t.Fatalf("explicit=%v error=%v diagnostics=%v", explicit, err, diagnostics)
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
		img, err := render.NewDocument(color.White, ofd.Documents[0]).RasterizePage(page, render.BackendCanvas, geom.DPMM(4))
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []color.RGBA{{B: 255, A: 255}, {G: 255, A: 255}} {
			x, y := float64(60+i*80)*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
			got := img.RGBAAt(int(x), int(y))
			if got != want {
				t.Fatalf("explicit=%v block %d=%v want %v", explicit, i, got, want)
			}
		}
	}
}

func TestPDFMaskBudgetIncludesDecodeAndCachedComposition(t *testing.T) {
	base := budgetImage(2, 2, "/Mask 6 0 R", bytes.Repeat([]byte{255}, 12))
	mask := maskImage(2, 2, "", []byte{0, 0})
	pdf := budgetImagePDF("/Im0 Do", base, mask)
	requireBudgetError(t, pdf, Limits{MaxTotalImagePixels: 11}, "total_image_pixels")
	var output bytes.Buffer
	// 基础图、遮罩与合成各 4 像素；重复 Do 命中合成后缓存，不再扣除。
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", base, mask), &output, "", Options{Limits: Limits{MaxTotalImagePixels: 12}}); err != nil {
		t.Fatal(err)
	}
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, maskImage(100, 100, "", []byte{0})), Limits{MaxImagePixels: 10}, "image_pixels")
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, maskImage(16, 16, "/Filter /FlateDecode", compressed.Bytes())), Limits{MaxDecodedStreamBytes: 16}, "decoded_stream_bytes")
	key := budgetImage(2, 2, "/Mask [0 0 0 0 0 0]", bytes.Repeat([]byte{255}, 12))
	requireBudgetError(t, budgetImagePDF("/Im0 Do", key), Limits{MaxTotalImagePixels: 7}, "total_image_pixels")
	requireBudgetError(t, budgetImagePDF("/Im0 Do", key), Limits{MaxOperations: 10}, "operations")
}

func TestPDFExplicitMaskKeepsHigherResolutionDetails(t *testing.T) {
	mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(4), "Height": types.Integer(2), "ImageMask": types.Boolean(true), "BitsPerComponent": types.Integer(1)}, Content: []byte{0x50, 0xa0}}
	stream := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "ColorSpace": types.Name("DeviceRGB"), "BitsPerComponent": types.Integer(8), "Mask": mask}, Content: []byte{240, 40, 20}}
	data, _, err := pdfImageData(nil, stream, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != image.Rect(0, 0, 4, 2) {
		t.Fatalf("mask resolution lost: %v", decoded.Bounds())
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			value := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if (value.A == 255) != (x%2 == y) || (value.A == 255 && (value.R != 240 || value.G != 40 || value.B != 20)) {
				t.Fatalf("pixel(%d,%d)=%v", x, y, value)
			}
		}
	}
	base := budgetImage(1, 1, "/Mask 6 0 R", []byte{240, 40, 20})
	maskPDF := maskImage(4, 2, "", []byte{0x50, 0xa0})
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, maskPDF), Limits{MaxTotalImagePixels: 16}, "total_image_pixels")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", base, maskPDF), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 17}}); err != nil {
		t.Fatal(err)
	}
	// 两个图的各轴最大值可能比任一原图更大，也必须检查单图上限。
	base = budgetImage(4, 1, "/Mask 6 0 R", bytes.Repeat([]byte{255}, 12))
	requireBudgetError(t, budgetImagePDF("/Im0 Do", base, maskImage(1, 4, "", []byte{0, 0, 0, 0})), Limits{MaxImagePixels: 4}, "image_pixels")
}

func TestPDFSMaskTakesPrecedenceOverMask(t *testing.T) {
	base := budgetImage(2, 2, "/SMask 6 0 R /Mask 7 0 R", bytes.Repeat([]byte{255}, 12))
	soft := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 4 >>\nstream\n%s\nendstream", []byte{255, 255, 255, 255})
	ignored := maskImage(1_000_000, 1_000_000, "", []byte{0})
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do", base, soft, ignored), &output, "", Options{Strict: true, Limits: Limits{MaxImagePixels: 4, MaxTotalImagePixels: 12}}); err != nil {
		t.Fatal(err)
	}
}

func TestPDFBrokenExplicitMaskReportsAndKeepsBaseImage(t *testing.T) {
	pdf := budgetImagePDF("/Im0 Do", budgetImage(2, 2, "/Mask 6 0 R", bytes.Repeat([]byte{255}, 12)), maskImage(2, 2, "", nil))
	for _, strict := range []bool{false, true} {
		var output bytes.Buffer
		var diagnostics []Diagnostic
		err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_explicit" || diagnostics[0].Page != 1 || diagnostics[0].Object != 5 || diagnostics[0].Resource != "Im0" {
			t.Fatalf("diagnostics=%+v", diagnostics)
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
			t.Fatal("broken mask removed base image")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, pdf, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel error=%v output=%d", err, output.Len())
	}
}

func TestPDFJPEGColorKeyStrictConversionMakesMatchedSamplesTransparent(t *testing.T) {
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewGray(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask [0 0] /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", jpegData.Len(), jpegData.Bytes())
	pdf := budgetImagePDF("/Im0 Do", base)
	var output bytes.Buffer
	var diagnostics []Diagnostic
	err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	if err != nil || output.Len() == 0 || len(diagnostics) != 0 {
		t.Fatalf("error=%v diagnostics=%v", err, diagnostics)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(0, 0); got.A != 0 {
		t.Fatalf("JPEG key did not remove black sample: %v", got)
	}
}

func TestPDFBrokenSoftMaskPreflightKeepsBaseImage(t *testing.T) {
	// 尺寸错误必须报告在软掩码上；普通模式不能连基础图片一起丢掉。
	pdf := budgetImagePDF("/Im0 Do", budgetImage(2, 2, "/SMask 6 0 R", bytes.Repeat([]byte{255}, 12)), budgetImage(0, 2, "", nil))
	var output bytes.Buffer
	var diagnostics []Diagnostic
	if err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask" {
		t.Fatalf("diagnostics=%v", diagnostics)
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
		t.Fatal("invalid soft mask removed base image")
	}
}

func TestPDFJBIG2ExplicitMaskHonorsDecode(t *testing.T) {
	for _, invert := range []bool{false, true} {
		opts, _ := (Options{Strict: true}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
		mask := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(3562), "Height": types.Integer(851), "ImageMask": types.Boolean(true), "BitsPerComponent": types.Integer(1)}, FilterPipeline: []types.PDFFilter{{Name: "JBIG2Decode"}}, Raw: []byte(jbig2SampleMask)}
		if invert {
			mask.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
		}
		base := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(2), "Height": types.Integer(1), "ColorSpace": types.Name("DeviceRGB"), "BitsPerComponent": types.Integer(8), "Mask": mask}, Content: []byte{255, 0, 0, 0, 255, 0}}
		data, format, err := p.imageData(base, pdfColor{})
		if err != nil || format != "PNG" {
			t.Fatalf("format=%s error=%v", format, err)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for x := 0; x < 2; x++ {
			_, _, _, alpha := decoded.At(x, 0).RGBA()
			if (alpha == 65535) == invert {
				t.Fatalf("invert=%v alpha=%d", invert, alpha)
			}
		}
	}
}

func TestPDFJPEGBaseSupportsExplicitMask(t *testing.T) {
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewGray(image.Rect(0, 0, 2, 1)), nil); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask 6 0 R /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", jpegData.Len(), jpegData.Bytes())
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("q 160 0 0 80 20 60 cm /Im0 Do Q", base, maskImage(2, 1, "", []byte{0x40})), &output, "", Options{Strict: true}); err != nil {
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
	img, err := render.NewDocument(color.White, ofd.Documents[0]).RasterizePage(page, render.BackendCanvas, geom.DPMM(4))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []color.RGBA{{A: 255}, {R: 255, G: 255, B: 255, A: 255}} {
		y := 100 * pdfPointToMillimeter * 4
		got := img.RGBAAt(int(float64(60+i*80)*pdfPointToMillimeter*4), int(y))
		if got != want {
			t.Fatalf("JPEG block %d=%v want %v", i, got, want)
		}
	}
}

func TestPDFInvalidColorKeyReportsInStrictMode(t *testing.T) {
	// pdfcpu 会先拒绝部分无效数组；这里直接保护内容解释器的校验与诊断。
	for _, ranges := range []types.Array{
		{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
		{types.Integer(256), types.Integer(256), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
		{types.Float(0.5), types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
		{types.Integer(0), types.Integer(0)},
	} {
		var diagnostics []Diagnostic
		opts, _ := (Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
		stream := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1), "Height": types.Integer(1), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceRGB"), "Mask": ranges}, Content: []byte{0, 0, 0}}
		_, _, err := p.imageData(stream, pdfColor{})
		if !errors.Is(err, ErrUnsupported) || len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_explicit" {
			t.Fatalf("ranges=%v error=%v diagnostics=%v", ranges, err, diagnostics)
		}
	}
}
