package pdf2ofd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func jpxDecodedImage(t *testing.T, p *pdfInterpreter, stream *types.StreamDict) image.Image {
	t.Helper()
	data, format, err := p.imageData(stream, pdfColor{r: 255})
	if err != nil || format != "PNG" {
		t.Fatalf("format=%s error=%v", format, err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestPDFJPXIgnoresDecodeAndDictionaryDepthForOrdinaryImages(t *testing.T) {
	for _, format := range []jpeg2000.Format{jpeg2000.FormatJP2, jpeg2000.FormatJ2K} {
		for _, cs := range []string{"DeviceGray", "DeviceRGB", "DeviceCMYK"} {
			t.Run(fmt.Sprintf("%v_%s", format, cs), func(t *testing.T) {
				samples, want := []uint16{0x12, 0x34, 0x56}, color.NRGBA64{R: 0x1212, G: 0x3434, B: 0x5656, A: 65535}
				if cs == "DeviceGray" {
					samples, want = []uint16{0x12}, color.NRGBA64{R: 0x1212, G: 0x1212, B: 0x1212, A: 65535}
				} else if cs == "DeviceCMYK" {
					samples = []uint16{0x12, 0x34, 0x56, 0x78}
					r, g, b := cmykInkToRGB(0x12, 0x34, 0x56, 0x78)
					want = color.NRGBA64{R: uint16(r) * 257, G: uint16(g) * 257, B: uint16(b) * 257, A: 65535}
				}
				stream := jpxMatteStream(cs, matteJPX(t, cs, 8, samples, format))
				stream.Dict["BitsPerComponent"] = types.Integer(16)
				decode := types.Array{}
				for range samples {
					decode = append(decode, types.Integer(1), types.Integer(0))
				}
				stream.Dict["Decode"] = decode
				if format == jpeg2000.FormatJP2 {
					delete(stream.Dict, "ColorSpace")
				}
				options, _ := (Options{Strict: true}).Normalize()
				p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
				img := jpxDecodedImage(t, &p, stream)
				if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != want {
					t.Fatalf("JPX ordinary pixel=%#v want %#v", got, want)
				}
			})
		}
	}
}

func TestPDFJPXPreservesHighDepthOrdinaryColour(t *testing.T) {
	for _, bits := range []int{9, 12, 16} {
		for _, cs := range []string{"DeviceGray", "DeviceCMYK"} {
			t.Run(fmt.Sprintf("%d_%s", bits, cs), func(t *testing.T) {
				samples := []uint16{0x1234}
				if cs == "DeviceCMYK" {
					samples = []uint16{0x1234, 0x3456, 0x5678, 0x789a}
				}
				stream := jpxMatteStream(cs, matteJPX(t, cs, bits, samples, jpeg2000.FormatJP2))
				delete(stream.Dict, "BitsPerComponent")
				img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
				maximum := (1 << bits) - 1
				normalize := func(value uint16) uint16 {
					encoded := int(value) * maximum / 65535
					return floatToUint16(float64(encoded) / float64(maximum))
				}
				r, g, b := normalize(samples[0]), normalize(samples[0]), normalize(samples[0])
				if cs == "DeviceCMYK" {
					converter := pdfImageCMYKConverter(nil, stream)
					r, g, b = converter.toRGB16(normalize(samples[0]), normalize(samples[1]), normalize(samples[2]), normalize(samples[3]))
				}
				if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: r, G: g, B: b, A: 65535}) {
					t.Fatalf("JPX high depth pixel=%#v", got)
				}
			})
		}
	}
}

func TestPDFJPXSoftMaskKeeps16BitAlphaAndResamplesActualDimensions(t *testing.T) {
	gray := image.NewGray16(image.Rect(0, 0, 2, 1))
	gray.SetGray16(0, 0, color.Gray16{Y: 32768})
	gray.SetGray16(1, 0, color.Gray16{Y: 32769})
	for _, base16 := range []bool{false, true} {
		mask := jpxMatteStream("DeviceGray", encodeJPX(t, gray, jpeg2000.FormatJP2))
		// 码流实际 2×1；不依据错误字典位深压成 8 位，Decode 同样应忽略。
		mask.Dict["Width"], mask.Dict["Height"], mask.Dict["BitsPerComponent"] = types.Integer(1), types.Integer(1), types.Integer(1)
		mask.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
		delete(mask.Dict, "ColorSpace")
		base := jpegMatteStream("DeviceRGB", bytes.Repeat([]byte{255, 0, 0}, 64), false)
		if base16 {
			base.Dict["BitsPerComponent"] = types.Integer(16)
			base.Content = bytes.Repeat(softMaskSamples16(0x1234, 0x3456, 0x5678), 64)
		}
		base.Dict["SMask"] = mask
		options, _ := (Options{Strict: true}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		img := jpxDecodedImage(t, &p, base)
		if _, ok := img.(*image.NRGBA64); !ok {
			t.Fatalf("JPX alpha precision lost: %T", img)
		}
		for x := 0; x < 8; x++ {
			want := color.NRGBA64{R: 65535, A: 32768}
			if base16 {
				want.R, want.G, want.B = 0x1234, 0x3456, 0x5678
			}
			if x >= 4 {
				want.A = 32769
			}
			if got := color.NRGBA64Model.Convert(img.At(x, 0)).(color.NRGBA64); got != want {
				t.Fatalf("base16=%v pixel %d=%#v want %#v", base16, x, got, want)
			}
		}
	}
}

func jpxAlphaStream(t *testing.T, bits, mode int, pixel color.NRGBA64) *types.StreamDict {
	t.Helper()
	var src image.Image
	if bits == 16 {
		rgba := image.NewNRGBA64(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				rgba.SetNRGBA64(x, y, pixel)
			}
		}
		src = rgba
	} else {
		rgba := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				rgba.SetNRGBA(x, y, color.NRGBA{R: byte(pixel.R / 257), G: byte(pixel.G / 257), B: byte(pixel.B / 257), A: byte(pixel.A / 257)})
			}
		}
		src = rgba
	}
	s := jpxMatteStream("DeviceRGB", encodeJPX(t, src, jpeg2000.FormatJP2))
	s.Dict["SMaskInData"] = types.Integer(mode)
	s.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
	return s
}

func TestPDFJPXEmbeddedAlphaIsNonPremultipliedInPNG(t *testing.T) {
	for _, bits := range []int{8, 16} {
		for _, mode := range []int{1, 2} {
			for _, transparent := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d_mode%d_zero%v", bits, mode, transparent), func(t *testing.T) {
					alpha := uint16(32769)
					if bits == 8 {
						alpha = 128 * 257
					}
					pixel := color.NRGBA64{R: 65535, A: alpha}
					if mode == 2 {
						pixel.R = alpha
					}
					if transparent {
						pixel = color.NRGBA64{}
					}
					s := jpxAlphaStream(t, bits, mode, pixel)
					options, _ := (Options{Strict: true}).Normalize()
					p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
					img := jpxDecodedImage(t, &p, s)
					want := color.NRGBA64{R: 65535, A: alpha}
					if transparent {
						want = color.NRGBA64{}
					}
					if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != want {
						t.Fatalf("embedded alpha=%#v want %#v", got, want)
					}
				})
			}
		}
	}
}

func TestPDFJPXDefaultSMaskInDataIgnoresExtraAlpha(t *testing.T) {
	stream := jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 0x1234, G: 0x3456, B: 0x5678, A: 32769})
	delete(stream.Dict, "SMaskInData")
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: 0x1234, G: 0x3456, B: 0x5678, A: 65535}) {
		t.Fatalf("default SMaskInData used alpha: %#v", got)
	}
}

func jpxWithChannelDefinitions(t *testing.T, data []byte, colours, components, mode int) []byte {
	t.Helper()
	entries := make([]byte, 2+components*6)
	binary.BigEndian.PutUint16(entries, uint16(components))
	for c := 0; c < components; c++ {
		entry := entries[2+c*6:]
		binary.BigEndian.PutUint16(entry, uint16(c))
		if c < colours {
			binary.BigEndian.PutUint16(entry[4:], uint16(c+1))
		} else {
			if mode == 0 {
				mode = 1
			}
			binary.BigEndian.PutUint16(entry[2:], uint16(mode))
		}
	}
	box := matteJP2Box("cdef", entries, false)
	for pos := 0; pos+8 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[pos:]))
		if n < 8 || pos+n > len(data) {
			t.Fatal("invalid JP2 box")
		}
		if string(data[pos+4:pos+8]) == "jp2h" {
			out := append([]byte(nil), data[:pos+n]...)
			out = append(out, box...)
			out = append(out, data[pos+n:]...)
			binary.BigEndian.PutUint32(out[pos:], uint32(n+len(box)))
			return out
		}
		pos += n
	}
	t.Fatal("missing jp2h")
	return nil
}

func TestPDFJPXIdentityChannelDefinitionsPreserveColourAndOpacity(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		pixel := color.NRGBA64{R: 65535, A: 32769}
		if mode == 2 {
			pixel.R = pixel.A
		}
		s := jpxAlphaStream(t, 16, mode, pixel)
		s.Raw = jpxWithChannelDefinitions(t, s.Raw, 3, 4, mode)
		img := jpxDecodedImage(t, &pdfInterpreter{}, s)
		want := color.NRGBA64{R: 65535, A: 32769}
		if mode == 0 {
			want.A = 65535
		}
		if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != want {
			t.Fatalf("mode %d: %#v want %#v", mode, got, want)
		}
	}
	s := jpxMatteStream("DeviceCMYK", matteJPX(t, "DeviceCMYK", 16, []uint16{65535, 0, 0, 0}, jpeg2000.FormatJP2))
	s.Raw = jpxWithChannelDefinitions(t, s.Raw, 4, 4, 0)
	img := jpxDecodedImage(t, &pdfInterpreter{}, s)
	converter := pdfImageCMYKConverter(nil, s)
	r, g, b := converter.toRGB16(65535, 0, 0, 0)
	if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: r, G: g, B: b, A: 65535}) {
		t.Fatalf("CMYK cdef pixel=%#v", got)
	}
}

func TestPDFJPXChannelDefinitionsRejectDuplicateAssociationAndMismatchedOpacity(t *testing.T) {
	for _, kind := range []string{"duplicate association", "duplicate", "partial", "opacity mismatch"} {
		s := jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 65535, A: 32769})
		s.Raw = jpxWithChannelDefinitions(t, s.Raw, 3, 4, 1)
		pos := bytes.Index(s.Raw, []byte("cdef")) + 4
		switch kind {
		case "duplicate association":
			binary.BigEndian.PutUint16(s.Raw[pos+6:], 2)
		case "duplicate":
			binary.BigEndian.PutUint16(s.Raw[pos+8:], 0)
		case "partial":
			binary.BigEndian.PutUint16(s.Raw[pos:], 3)
		case "opacity mismatch":
			binary.BigEndian.PutUint16(s.Raw[pos+22:], 2)
		}
		p := pdfInterpreter{}
		if _, err := p.jpxHeader(s, true); err == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}

func TestPDFJPXIndependentOpenJPEGOpacityDefinition(t *testing.T) {
	// OpenJPEG 2.5.0 独立编码的 8×8 红色、alpha 128 图像，包含标准 cdef。
	// 同时验证 PNG 与蓝背景实际渲染，避免同一 Go 编码器的假一致。
	data, err := base64.StdEncoding.DecodeString("AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAABPanAyaAAAABZpaGRyAAAACAAAAAgABAcHAAAAAAAPY29scgEAAAAAABAAAAAiY2RlZgAEAAAAAAABAAEAAAACAAIAAAADAAMAAQAAAAAAompwMmP/T/9RADIAAAAAAAgAAAAIAAAAAAAAAAAAAAAIAAAACAAAAAAAAAAAAAQHAQEHAQEHAQEHAQH/UgAMAAAAAQAABAQAAf9cAARAQP9kACUAAUNyZWF0ZWQgYnkgT3BlbkpQRUcgdmVyc2lvbiAyLjUuMP+QAAoAAAAAACcAAf+Tz7QUFABcr4ffgCgRUFSjb9+AKBFQVKNvgP/Z")
	if err != nil {
		t.Fatal(err)
	}
	s := jpxMatteStream("DeviceRGB", data)
	s.Dict["SMaskInData"] = types.Integer(1)
	img := jpxDecodedImage(t, &pdfInterpreter{}, s)
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 255, A: 128}) {
		t.Fatalf("OpenJPEG opacity=%v", got)
	}
	input := budgetImagePDF("0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /SMaskInData 1 /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data))
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	doc := render.NewDocument(color.White, ofd.Documents[0])
	defer doc.Close()
	raster, err := doc.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPMM(4))
	if err != nil {
		t.Fatal(err)
	}
	x, y := 100*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
	if got := raster.RGBAAt(int(x), int(y)); got.R < 126 || got.R > 128 || got.G != 0 || got.B < 127 || got.B > 129 {
		t.Fatalf("OpenJPEG source colour differs: %v", got)
	}
}

func TestPDFJPXOrdinaryKeepsExistingYCbCrColourConversion(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 100, G: 128, B: 128, A: 255})
		}
	}
	options := jpeg2000.DefaultOptions()
	options.Lossless, options.ColorSpace = true, jpeg2000.ColorSpaceSYCC
	var buffer bytes.Buffer
	if err := jpeg2000.Encode(&buffer, matteJPXRGB{src}, options); err != nil {
		t.Fatal(err)
	}
	stream := jpxMatteStream("DeviceRGB", buffer.Bytes())
	stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
	opts, _ := (Options{Strict: true}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: opts}}
	img := jpxDecodedImage(t, &p, stream)
	// 中性 Cb/Cr 对应灰度，确认既有转换未被原分量路径误拒绝或反相。
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got.R < 98 || got.R > 102 || got.G < 98 || got.G > 102 || got.B < 98 || got.B > 102 || got.A != 255 {
		t.Fatalf("existing sYCC colour conversion changed: %v", got)
	}
}

func TestPDFJPX16BitRGBPreservesVaryingSamples(t *testing.T) {
	src := image.NewNRGBA64(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.SetNRGBA64(x, y, color.NRGBA64{R: uint16(x*8191 + y), G: uint16(y*8191 + x), B: uint16(x*257 + y*4096), A: 65535})
		}
	}
	s := jpxMatteStream("DeviceRGB", encodeJPX(t, src, jpeg2000.FormatJP2))
	s.Dict["BitsPerComponent"] = types.Integer(1)
	s.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
	img := jpxDecodedImage(t, &pdfInterpreter{}, s)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want := src.NRGBA64At(x, y)
			if got := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64); got != want {
				t.Fatalf("16-bit RGB (%d,%d)=%#v want %#v", x, y, got, want)
			}
		}
	}
}

func TestPDFJPXUnsupportedSoftMaskReportsAndKeepsBase(t *testing.T) {
	mask := matteJPX(t, "DeviceRGB", 8, []uint16{128, 128, 128}, jpeg2000.FormatJP2)
	input := budgetImagePDF("/Im0 Do", budgetImage(8, 8, "/SMask 6 0 R", bytes.Repeat([]byte{255, 0, 0}, 64)),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(mask), mask))
	for _, strict := range []bool{false, true} {
		var diagnostics []Diagnostic
		var output bytes.Buffer
		err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask" || diagnostics[0].Object != 5 {
			t.Fatalf("diagnostics=%v", diagnostics)
		}
		if strict {
			if !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
				t.Fatalf("strict error=%v output=%d", err, output.Len())
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			img := decodeFirstPageImage(t, output.Bytes())
			if got := img.NRGBAAt(4, 4); got != (color.NRGBA{R: 255, A: 255}) {
				t.Fatalf("unsupported mask lost base: %v", got)
			}
		}
	}
}

func TestPDFJPXStencilAndExplicitMaskKeepDecodeInversion(t *testing.T) {
	for _, invert := range []bool{false, true} {
		mask := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 1, []uint16{0}, jpeg2000.FormatJP2))
		mask.Dict["ImageMask"], mask.Dict["BitsPerComponent"] = types.Boolean(true), types.Integer(1)
		delete(mask.Dict, "ColorSpace")
		if invert {
			mask.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
		}
		options, _ := (Options{Strict: true}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		img := jpxDecodedImage(t, &p, mask)
		_, _, _, a := img.At(4, 4).RGBA()
		if (a == 0) != invert {
			t.Fatalf("stencil invert=%v alpha=%d", invert, a)
		}
		base := jpegMatteStream("DeviceRGB", bytes.Repeat([]byte{255, 0, 0}, 64), false)
		base.Dict["Mask"] = mask
		p = pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		img = jpxDecodedImage(t, &p, base)
		_, _, _, a = img.At(4, 4).RGBA()
		if (a == 0) != invert {
			t.Fatalf("explicit invert=%v alpha=%d", invert, a)
		}
	}
}

func ordinaryJPXPDF(t *testing.T, content string, alpha bool) []byte {
	t.Helper()
	base := matteJPX(t, "DeviceRGB", 8, []uint16{0x12, 0x34, 0x56}, jpeg2000.FormatJ2K)
	extra := "/ColorSpace /DeviceRGB /Decode [1 0 1 0 1 0]"
	if alpha {
		base = jpxAlphaStream(t, 16, 2, color.NRGBA64{R: 32769, A: 32769}).Raw
		extra += " /SMaskInData 2"
	}
	return budgetImagePDF(content, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 %s /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", extra, len(base), base))
}

func TestPDFJPXOrdinaryStrictRenderingKeepsColourAndEmbeddedAlpha(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		input := ordinaryJPXPDF(t, "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", alpha)
		var output bytes.Buffer
		if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
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
		got := img.RGBAAt(int(x), int(y))
		want := color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 255}
		if alpha {
			want = color.RGBA{R: 128, B: 127, A: 255}
		}
		for i, value := range []byte{got.R, got.G, got.B} {
			w := []byte{want.R, want.G, want.B}[i]
			if d := int(value) - int(w); d < -1 || d > 1 {
				t.Fatalf("alpha=%v render=%v want %v", alpha, got, want)
			}
		}
	}
}

func TestPDFJPXOrdinaryProcessingBudgetAndCache(t *testing.T) {
	input := ordinaryJPXPDF(t, "/Im0 Do", false)
	requireBudgetError(t, input, Limits{MaxOperations: 257}, "operations")
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 63}, "total_image_pixels")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), ordinaryJPXPDF(t, "/Im0 Do /Im0 Do", false), &output, "", Options{Strict: true, Limits: Limits{MaxOperations: 260, MaxTotalImagePixels: 64}}); err != nil {
		t.Fatal(err)
	}
}

func TestPDFJPXUnsupportedStructureReportsBeforeSkippingImage(t *testing.T) {
	// 有符号码流尚未支持；不能默默按无符号样本解释后继续。
	s := jpxAlphaStream(t, 8, 1, color.NRGBA64{R: 65535, A: 32896})
	pos := bytes.Index(s.Raw, []byte{0xff, 0x51})
	if pos < 0 {
		t.Fatal("missing SIZ")
	}
	s.Raw[pos+40] |= 0x80
	input := budgetImagePDF("/Im0 Do 0 0 1 rg 0 0 200 200 re f", fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /SMaskInData 1 /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(s.Raw), s.Raw))
	for _, strict := range []bool{false, true} {
		var diagnostics []Diagnostic
		var output bytes.Buffer
		err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.decode" || diagnostics[0].Object != 5 {
			t.Fatalf("diagnostics=%v", diagnostics)
		}
		if strict {
			if !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
				t.Fatalf("strict err=%v output=%d", err, output.Len())
			}
		} else if err != nil || output.Len() == 0 {
			t.Fatalf("normal err=%v output=%d", err, output.Len())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel err=%v output=%d", err, output.Len())
	}
}
