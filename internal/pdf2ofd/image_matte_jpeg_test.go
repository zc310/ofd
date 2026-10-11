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
	"github.com/zc310/ofd/internal/render/geom"
)

func matteJPEG(t *testing.T, gray bool, value color.NRGBA) []byte {
	t.Helper()
	var src image.Image
	if gray {
		g := image.NewGray(image.Rect(0, 0, 8, 8))
		for i := range g.Pix {
			g.Pix[i] = value.R
		}
		src = g
	} else {
		rgb := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				rgb.SetNRGBA(x, y, value)
			}
		}
		src = rgb
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func jpegMatteStream(cs string, data []byte, encoded bool) *types.StreamDict {
	s := &types.StreamDict{Dict: types.Dict{"Width": types.Integer(8), "Height": types.Integer(8), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name(cs)}, Content: data}
	if encoded {
		s.Raw = data
		s.FilterPipeline = []types.PDFFilter{{Name: "DCTDecode"}}
	}
	return s
}

func TestPDFJPEGMatteRestoresColourWithRawOrJPEGMask(t *testing.T) {
	for _, gray := range []bool{false, true} {
		for _, jpegBase := range []bool{false, true} {
			for _, jpegMask := range []bool{false, true} {
				if !jpegBase && !jpegMask {
					continue
				}
				t.Run(fmt.Sprintf("gray%v_base%v_mask%v", gray, jpegBase, jpegMask), func(t *testing.T) {
					cs, pixel, matte := "DeviceRGB", color.NRGBA{255, 127, 127, 255}, types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
					baseData := bytes.Repeat([]byte{255, 127, 127}, 64)
					if gray {
						cs, pixel, matte = "DeviceGray", color.NRGBA{128, 128, 128, 255}, types.Array{types.Integer(0)}
						baseData = bytes.Repeat([]byte{128}, 64)
					}
					if jpegBase {
						baseData = matteJPEG(t, gray, pixel)
					}
					maskData := bytes.Repeat([]byte{128}, 64)
					if jpegMask {
						maskData = matteJPEG(t, true, color.NRGBA{R: 128, A: 255})
					}
					mask := jpegMatteStream("DeviceGray", maskData, jpegMask)
					mask.Dict["Matte"] = matte
					base := jpegMatteStream(cs, baseData, jpegBase)
					base.Dict["SMask"] = mask
					opts, _ := (Options{Strict: true}).Normalize()
					p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
					data, _, err := p.imageData(base, pdfColor{})
					if err != nil {
						t.Fatal(err)
					}
					img, _, err := image.Decode(bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					for y := 0; y < 8; y++ {
						for x := 0; x < 8; x++ {
							v := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
							if v.R < 250 || v.A != 128 || (!gray && (v.G > 5 || v.B > 5)) || (gray && (v.G < 250 || v.B < 250)) {
								t.Fatalf("pixel(%d,%d)=%v", x, y, v)
							}
						}
					}
				})
			}
		}
	}
}

func TestPDFJPEGMatteAppliesBaseAndMaskDecode(t *testing.T) {
	mask := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 127, A: 255}), true)
	mask.Dict["Matte"] = types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
	mask.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
	base := jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{0, 128, 128, 255}), true)
	base.Dict["SMask"] = mask
	base.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
	opts, _ := (Options{Strict: true}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
	data, _, err := p.imageData(base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if v := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); v.R < 250 || v.G > 5 || v.B > 5 || v.A != 128 {
		t.Fatalf("Decode order: %v", v)
	}
}

// constantMatteCMYKJPEG 构造独立的 8×8、四分量 JPEG。所有 AC 系数为零，
// DC 系数直接编码已知样本，不依赖外部样例或把 CMYK 转成 RGB 的 JPEG 编码器。
func constantMatteCMYKJPEG(samples [4]byte, transform byte) []byte {
	data := []byte{0xff, 0xd8}
	segment := func(marker byte, payload []byte) {
		n := len(payload) + 2
		data = append(data, 0xff, marker, byte(n>>8), byte(n))
		data = append(data, payload...)
	}
	segment(0xee, append([]byte("Adobe\x00\x64\x00\x00\x00\x00"), transform))
	segment(0xdb, append([]byte{0}, bytes.Repeat([]byte{1}, 64)...))
	segment(0xc0, []byte{8, 0, 8, 0, 8, 4, 1, 0x11, 0, 2, 0x11, 0, 3, 0x11, 0, 4, 0x11, 0})
	dc := make([]byte, 17)
	dc[4] = 12
	for i := byte(0); i < 12; i++ {
		dc = append(dc, i)
	}
	ac := make([]byte, 18)
	ac[0], ac[1] = 0x10, 1
	segment(0xc4, append(dc, ac...))
	segment(0xda, []byte{4, 1, 0, 2, 0, 3, 0, 4, 0, 0, 63, 0})
	var pending byte
	n := 0
	bit := func(v int) {
		pending = pending<<1 | byte(v&1)
		n++
		if n == 8 {
			data = append(data, pending)
			if pending == 255 {
				data = append(data, 0)
			}
			pending, n = 0, 0
		}
	}
	write := func(v, bits int) {
		for i := bits - 1; i >= 0; i-- {
			bit(v >> i)
		}
	}
	for _, sample := range samples {
		coefficient := (int(sample) - 128) * 8
		abs := coefficient
		if abs < 0 {
			abs = -abs
		}
		size := 0
		for abs != 0 {
			size++
			abs >>= 1
		}
		write(size, 4)
		if coefficient < 0 {
			coefficient += (1 << size) - 1
		}
		write(coefficient, size)
		write(0, 1)
	}
	for n != 0 {
		bit(1)
	}
	return append(data, 0xff, 0xd9)
}

func TestPDFJPEGMatteUnblendsAdobeCMYKAndYCCKBeforeRGB(t *testing.T) {
	for _, tc := range []struct {
		name      string
		samples   [4]byte
		transform byte
		decode    types.Array
		want      [4]byte
	}{
		{"CMYK", [4]byte{128, 0, 0, 0}, 0, nil, [4]byte{255, 0, 0, 0}},
		{"CMYK inverted Decode", [4]byte{127, 255, 255, 255}, 0, types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}, [4]byte{255, 0, 0, 0}},
		{"YCCK", [4]byte{127, 128, 128, 128}, 2, nil, [4]byte{255, 255, 255, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mask := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
			mask.Dict["Matte"] = types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}
			base := jpegMatteStream("DeviceCMYK", constantMatteCMYKJPEG(tc.samples, tc.transform), true)
			base.Dict["SMask"] = mask
			if tc.decode != nil {
				base.Dict["Decode"] = tc.decode
			}
			opts, _ := (Options{Strict: true}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
			data, _, err := p.imageData(base, pdfColor{})
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			r, g, b := cmykInkToRGB(tc.want[0], tc.want[1], tc.want[2], tc.want[3])
			want := color.NRGBA{R: r, G: g, B: b, A: 128}
			if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != want {
				t.Fatalf("CMYK Matte=%v want %v", got, want)
			}
		})
	}
}

func jpegMattePDF(t *testing.T, content string, outerFlate bool) []byte {
	t.Helper()
	base := matteJPEG(t, false, color.NRGBA{255, 127, 127, 255})
	mask := matteJPEG(t, true, color.NRGBA{R: 128, A: 255})
	filter := "/DCTDecode"
	if outerFlate {
		filter = "[/FlateDecode /DCTDecode]"
		compress := func(data []byte) []byte {
			var output bytes.Buffer
			w := zlib.NewWriter(&output)
			if _, err := w.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			return output.Bytes()
		}
		base, mask = compress(base), compress(mask)
	}
	return budgetImagePDF(content,
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R /Filter %s /Length %d >>\nstream\n%s\nendstream", filter, len(base), base),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8 /Matte [1 1 1] /Filter %s /Length %d >>\nstream\n%s\nendstream", filter, len(mask), mask))
}

func TestPDFJPEGMatteStrictConversionRendersWithoutFringe(t *testing.T) {
	for _, outerFlate := range []bool{false, true} {
		var output bytes.Buffer
		input := jpegMattePDF(t, "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", outerFlate)
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
		x, y := 100*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
		if v := img.RGBAAt(int(x), int(y)); v.R < 125 || v.R > 131 || v.G > 3 || v.B < 124 || v.B > 131 {
			t.Fatalf("outerFlate=%v white fringe: %v", outerFlate, v)
		}
	}
}

func TestPDFJPEGMatteBudgetsIncludeExtraDecodesAndCache(t *testing.T) {
	input := jpegMattePDF(t, "/Im0 Do", false)
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 319}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 577}, "operations")
	var output bytes.Buffer
	// 基础图、掩码、合成及两次额外 JPEG 解码各 64 像素，重复 Do 命中缓存。
	if err := ConvertWithOptions(t.Context(), jpegMattePDF(t, "/Im0 Do /Im0 Do", false), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 320, MaxOperations: 580}}); err != nil {
		t.Fatal(err)
	}
	base := jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 127, 127, 255}), true)
	base.Dict["Width"], base.Dict["Height"] = types.Integer(1), types.Integer(1)
	options, _ := (Options{Limits: Limits{MaxImagePixels: 4}}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	if _, _, err := p.imageData(base, pdfColor{}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("JPEG actual size budget=%v", err)
	}
}

func TestPDFJPEGMatteKeeps16BitMaskAlpha(t *testing.T) {
	mask := jpegMatteStream("DeviceGray", bytes.Repeat(softMaskSamples16(32768), 64), false)
	mask.Dict["BitsPerComponent"] = types.Integer(16)
	mask.Dict["Matte"] = types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
	base := jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 127, 127, 255}), true)
	base.Dict["SMask"] = mask
	options, _ := (Options{Strict: true}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	data, _, err := p.imageData(base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.NRGBA64); !ok {
		t.Fatalf("16-bit alpha lost: %T", img)
	}
	if v := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); v.A != 32768 || v.R < 64000 || v.G > 1024 || v.B > 1024 {
		t.Fatalf("16-bit JPEG matte=%#v", v)
	}
}

func TestPDFJPEGMatteReportsMismatchedSamplesAndTransform(t *testing.T) {
	for _, kind := range []string{"dimensions", "mask components", "ColorTransform", "bits"} {
		var diagnostics []Diagnostic
		options, _ := (Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}, source: Diagnostic{Page: 1, Object: 5, Resource: "Im0"}}
		base := jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 127, 127, 255}), true)
		mask := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 128, A: 255}), true)
		mask.Dict["Matte"] = types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
		base.Dict["SMask"] = mask
		switch kind {
		case "dimensions":
			base.Dict["Width"] = types.Integer(1)
			mask.Dict["Width"] = types.Integer(1)
		case "mask components":
			mask.Raw = base.Raw
			mask.Content = base.Content
		case "ColorTransform":
			base.FilterPipeline[0].DecodeParms = types.Dict{"ColorTransform": types.Integer(0)}
		case "bits":
			base.Dict["BitsPerComponent"] = types.Integer(16)
		}
		if _, _, err := p.imageData(base, pdfColor{}); !errors.Is(err, ErrUnsupported) || len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_matte" || diagnostics[0].Object != 5 {
			t.Fatalf("%s err=%v diagnostics=%v", kind, err, diagnostics)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	input := jpegMattePDF(t, "/Im0 Do", false)
	// 在有效 JPEG 上制造 Matte 参数错误，回调取消仍须原样返回 context 错误。
	input = bytes.Replace(input, []byte("/Matte [1 1 1]"), []byte("/Matte [1]"), 1)
	err := ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel=%v output=%d", err, output.Len())
	}
}
