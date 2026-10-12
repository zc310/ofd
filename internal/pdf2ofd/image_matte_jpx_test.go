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

// 编码器对标准 RGBA 类型固定写四分量；通过不含 alpha 的颜色模型生成三分量
// RGB，避免把额外透明度通道当作 DeviceRGB 原颜色分量。
type matteJPXRGB struct{ *image.NRGBA }

func (matteJPXRGB) ColorModel() color.Model {
	return color.ModelFunc(func(c color.Color) color.Color {
		r, g, b, _ := c.RGBA()
		return color.RGBA{byte(r >> 8), byte(g >> 8), byte(b >> 8), 255}
	})
}

func matteJPX(t *testing.T, cs string, bits int, samples []uint16, format jpeg2000.Format) []byte {
	t.Helper()
	var src image.Image
	options := jpeg2000.DefaultOptions()
	options.Lossless, options.Format, options.Precision = true, format, bits
	switch cs {
	case "DeviceGray":
		options.ColorSpace = jpeg2000.ColorSpaceGray
		if bits > 8 {
			gray := image.NewGray16(image.Rect(0, 0, 8, 8))
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					gray.SetGray16(x, y, color.Gray16{Y: samples[0]})
				}
			}
			src = gray
		} else {
			gray := image.NewGray(image.Rect(0, 0, 8, 8))
			for i := range gray.Pix {
				gray.Pix[i] = byte(samples[0])
			}
			src = gray
		}
	case "DeviceRGB":
		options.ColorSpace = jpeg2000.ColorSpaceSRGB
		rgb := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				rgb.SetNRGBA(x, y, color.NRGBA{byte(samples[0]), byte(samples[1]), byte(samples[2]), 255})
			}
		}
		src = matteJPXRGB{rgb}
	case "DeviceCMYK":
		// 用编码器的四分量通道传递 CMYK 整数，不经 Go CMYK→RGB 转换。
		options.ColorSpace = jpeg2000.ColorSpaceCMYK
		if bits > 8 {
			cmyk := image.NewNRGBA64(image.Rect(0, 0, 8, 8))
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					cmyk.SetNRGBA64(x, y, color.NRGBA64{samples[0], samples[1], samples[2], samples[3]})
				}
			}
			src = cmyk
		} else {
			cmyk := image.NewNRGBA(image.Rect(0, 0, 8, 8))
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					cmyk.SetNRGBA(x, y, color.NRGBA{byte(samples[0]), byte(samples[1]), byte(samples[2]), byte(samples[3])})
				}
			}
			src = cmyk
		}
	default:
		t.Fatalf("unknown colour space %s", cs)
	}
	var data bytes.Buffer
	if err := jpeg2000.Encode(&data, src, options); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func jpxMatteStream(cs string, data []byte) *types.StreamDict {
	stream := jpegMatteStream(cs, nil, false)
	stream.Raw, stream.FilterPipeline = data, []types.PDFFilter{{Name: "JPXDecode"}}
	return stream
}

func TestPDFJPXMatteReadsIndependentOpenJPEGIntegerPlanes(t *testing.T) {
	// OpenJPEG 2.5.0 生成的 8×8 常量图；与本项目使用的 Go 编码器独立。
	// 固定字节使回归不依赖 ImageMagick/OpenJPEG 的运行环境。
	for _, tc := range []struct {
		cs, encoded string
		bits        int
		want        []float32
	}{
		{"DeviceRGB", "AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAAAtanAyaAAAABZpaGRyAAAACAAAAAgAAwcHAAAAAAAPY29scgEAAAAAABAAAACXanAyY/9P/1EALwAAAAAACAAAAAgAAAAAAAAAAAAAAAgAAAAIAAAAAAAAAAAAAwcBAQcBAQcBAf9SAAwAAAABAQAEBAAB/1wABEBA/2QAJQABQ3JlYXRlZCBieSBPcGVuSlBFRyB2ZXJzaW9uIDIuNS4w/5AACgAAAAAAHwAB/5PD5woUAFyvh4DfgCgUAFyjb//Z", 8, []float32{255, 127, 127}},
		{"DeviceGray", "AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAAAtanAyaAAAABZpaGRyAAAACAAAAAgAAQ8HAAAAAAAPY29scgEAAAAAABEAAACHanAyY/9P/1EAKQAAAAAACAAAAAgAAAAAAAAAAAAAAAgAAAAIAAAAAAAAAAAAAQ8BAf9SAAwAAAABAAAEBAAB/1wABECA/2QAJQABQ3JlYXRlZCBieSBPcGVuSlBFRyB2ZXJzaW9uIDIuNS4w/5AACgAAAAAAFQAB/5PAACQUAFyf/9k=", 16, []float32{32769}},
	} {
		data, err := base64.StdEncoding.DecodeString(tc.encoded)
		if err != nil {
			t.Fatal(err)
		}
		options, _ := (Options{Strict: true}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		stream := jpxMatteStream(tc.cs, data)
		info, err := p.matteJPXHeader(stream)
		if err != nil {
			t.Fatal(err)
		}
		if info.metadata.BitsPerComponent[0] != tc.bits {
			t.Fatalf("OpenJPEG depth=%v", info.metadata.BitsPerComponent)
		}
		planes, err := p.matteJPXImage(stream, info, 8, 8)
		if err != nil {
			t.Fatal(err)
		}
		for c, want := range tc.want {
			for i, value := range planes.Components[c] {
				if value != want {
					t.Fatalf("%s component %d sample %d=%v want %v", tc.cs, c, i, value, want)
				}
			}
		}
	}
}

func TestPDFJPXMatteRestoresRawRGBGrayAndCMYKComponents(t *testing.T) {
	for _, format := range []jpeg2000.Format{jpeg2000.FormatJP2, jpeg2000.FormatJ2K} {
		for _, cs := range []string{"DeviceGray", "DeviceRGB", "DeviceCMYK"} {
			for _, jpxBase := range []bool{false, true} {
				for _, jpxMask := range []bool{false, true} {
					if !jpxBase && !jpxMask {
						continue
					}
					t.Run(fmt.Sprintf("%v_%s_base%v_mask%v", format, cs, jpxBase, jpxMask), func(t *testing.T) {
						samples, matte := []uint16{255, 127, 127}, types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
						want := color.NRGBA{R: 255, A: 128}
						if cs == "DeviceGray" {
							samples, matte, want.G, want.B = []uint16{128}, types.Array{types.Integer(0)}, 255, 255
						} else if cs == "DeviceCMYK" {
							samples, matte = []uint16{128, 0, 0, 0}, types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}
							want.R, want.G, want.B = cmykInkToRGB(255, 0, 0, 0)
						}
						raw := make([]byte, len(samples))
						for i, sample := range samples {
							raw[i] = byte(sample)
						}
						base := jpegMatteStream(cs, bytes.Repeat(raw, 64), false)
						mask := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
						if jpxBase {
							base = jpxMatteStream(cs, matteJPX(t, cs, 8, samples, format))
						}
						if jpxMask {
							mask = jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 8, []uint16{128}, format))
						}
						base.Dict["SMask"], mask.Dict["Matte"] = mask, matte
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
						for y := 0; y < 8; y++ {
							for x := 0; x < 8; x++ {
								if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got != want {
									t.Fatalf("pixel(%d,%d)=%v want %v", x, y, got, want)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestPDFJPXMatteUsesCodestreamDepthAndIgnoresPDFDecode(t *testing.T) {
	for _, bits := range []int{1, 2, 4, 8, 12, 16} {
		t.Run(fmt.Sprintf("%d_bit", bits), func(t *testing.T) {
			sample, originalMax := uint16(128), 255
			if bits > 8 {
				sample, originalMax = 32768, 65535
			}
			base := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", bits, []uint16{sample}, jpeg2000.FormatJP2))
			mask := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", bits, []uint16{sample}, jpeg2000.FormatJP2))
			base.Dict["SMask"], mask.Dict["Matte"] = mask, types.Array{types.Integer(0)}
			// JPX 的两项字典参数都须忽略；颜色空间则可从 JP2 推导。
			for _, stream := range []*types.StreamDict{base, mask} {
				delete(stream.Dict, "ColorSpace")
				delete(stream.Dict, "BitsPerComponent")
				stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
			}
			data, _, err := pdfImageData(nil, base, pdfColor{})
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			maximum := (1 << bits) - 1
			encoded := int(sample) * maximum / originalMax
			want := color.NRGBA64{R: 65535, G: 65535, B: 65535, A: floatToUint16(float64(encoded) / float64(maximum))}
			if bits <= 8 {
				want.A = uint16(floatToByte(float64(encoded)/float64(maximum))) * 257
			}
			if encoded == 0 {
				want = color.NRGBA64{}
			}
			if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != want {
				t.Fatalf("JPX precision=%#v want %#v", got, want)
			}
			if bits > 8 {
				if _, ok := img.(*image.NRGBA64); !ok {
					t.Fatalf("16-bit output lost: %T", img)
				}
			}
		})
	}
}

func TestPDFJPXMattePreserves16BitCMYKComponentsAndAlpha(t *testing.T) {
	base := jpxMatteStream("DeviceCMYK", matteJPX(t, "DeviceCMYK", 16, []uint16{0x1234, 0x3456, 0x5678, 0x789a}, jpeg2000.FormatJP2))
	mask := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 16, []uint16{65535}, jpeg2000.FormatJP2))
	base.Dict["SMask"], mask.Dict["Matte"] = mask, types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}
	// 码流有 16 位分量，PDF 的 8 位声明不应截断它。
	data, _, err := pdfImageData(nil, base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	converter := pdfImageCMYKConverter(nil, base)
	r, g, b := converter.toRGB16(0x1234, 0x3456, 0x5678, 0x789a)
	if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{r, g, b, 65535}) {
		t.Fatalf("CMYK components lost: %#v", got)
	}
	mask.Raw = matteJPX(t, "DeviceGray", 16, []uint16{32769}, jpeg2000.FormatJP2)
	base.Raw = matteJPX(t, "DeviceCMYK", 16, []uint16{32769, 0, 0, 0}, jpeg2000.FormatJP2)
	data, _, err = pdfImageData(nil, base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err = image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b = converter.toRGB16(65535, 0, 0, 0)
	if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{r, g, b, 32769}) {
		t.Fatalf("CMYK unblending or alpha lost: %#v", got)
	}
}

func TestPDFJPXMatteSupportsJPEGAndJBIG2Inputs(t *testing.T) {
	for _, kind := range []string{"JPEG base", "JPEG mask", "JBIG2 mask"} {
		t.Run(kind, func(t *testing.T) {
			base := jpxMatteStream("DeviceRGB", matteJPX(t, "DeviceRGB", 8, []uint16{255, 127, 127}, jpeg2000.FormatJP2))
			mask := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 16, []uint16{32768}, jpeg2000.FormatJP2))
			switch kind {
			case "JPEG base":
				base = jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 127, 127, 255}), true)
			case "JPEG mask":
				mask = jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 128, A: 255}), true)
			case "JBIG2 mask":
				mask = jbig2MatteStream(false)
				mask.Dict["Decode"] = types.Array{types.Integer(0), types.Float(.5)}
			}
			base.Dict["SMask"], mask.Dict["Matte"] = mask, types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
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
			got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA)
			if got.R < 250 || got.G > 5 || got.B > 5 || got.A != 128 {
				t.Fatalf("pixel=%v", got)
			}
		})
	}
}

func jpxMattePDF(t *testing.T, content string, format jpeg2000.Format) []byte {
	t.Helper()
	base := matteJPX(t, "DeviceRGB", 8, []uint16{255, 127, 127}, format)
	mask := matteJPX(t, "DeviceGray", 16, []uint16{32768}, format)
	// JPX 对 Decode 不生效；保留反相数组保护此规范差异。
	return budgetImagePDF(content,
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /SMask 6 0 R /Decode [1 0 1 0 1 0] /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(base), base),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /Matte [1 1 1] /Decode [1 0] /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(mask), mask))
}

func TestPDFJPXMatteStrictConversionRendersWithoutFringe(t *testing.T) {
	for _, format := range []jpeg2000.Format{jpeg2000.FormatJP2, jpeg2000.FormatJ2K} {
		var output bytes.Buffer
		input := jpxMattePDF(t, "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", format)
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
		if got := img.RGBAAt(int(x), int(y)); got.R < 126 || got.R > 130 || got.G > 2 || got.B < 125 || got.B > 130 {
			t.Fatalf("white fringe: %v", got)
		}
	}
}

func TestPDFJPXMatteStrictConversionInfersOmittedColourSpaces(t *testing.T) {
	input := jpxMattePDF(t, "/Im0 Do", jpeg2000.FormatJP2)
	input = bytes.ReplaceAll(input, []byte("/ColorSpace /DeviceRGB"), nil)
	input = bytes.ReplaceAll(input, []byte("/ColorSpace /DeviceGray"), nil)
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	img := decodeFirstPageImage(t, output.Bytes())
	if got := img.NRGBAAt(4, 4); got.R != 255 || got.G != 0 || got.B != 0 || got.A < 127 || got.A > 128 {
		t.Fatalf("inferred JP2 colours=%v", got)
	}
}

func TestPDFJPXMatteBudgetsIncludeExtraDecodesAndCache(t *testing.T) {
	input := jpxMattePDF(t, "/Im0 Do", jpeg2000.FormatJ2K)
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 319}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 833}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), jpxMattePDF(t, "/Im0 Do /Im0 Do", jpeg2000.FormatJ2K), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 320, MaxOperations: 836}}); err != nil {
		t.Fatal(err)
	}
	// JP2 盒扫描也受处理量预算控制，码流实际尺寸独立于 PDF 声明。
	base := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 8, []uint16{128}, jpeg2000.FormatJP2))
	options, _ := (Options{Limits: Limits{MaxOperations: 1}}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	if _, err := p.matteJPXHeader(base); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("JP2 header budget=%v", err)
	}
	options, _ = (Options{Limits: Limits{MaxStreamBytes: int64(len(base.Raw) - 1)}}).Normalize()
	p = pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	if _, err := p.matteJPXHeader(base); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("JP2 encoded stream budget=%v", err)
	}
	base.Dict["Width"], base.Dict["Height"] = types.Integer(1), types.Integer(1)
	options, _ = (Options{Limits: Limits{MaxImagePixels: 4}}).Normalize()
	p = pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	if _, _, err := p.imageData(base, pdfColor{}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("JPX actual dimensions budget=%v", err)
	}
}

func TestPDFJPXMatteReportsMismatchedAndUnsupportedMask(t *testing.T) {
	for _, kind := range []string{"dimensions", "components", "SMaskInData", "colourspace", "missing raw colourspace", "signed"} {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_strict%v", kind, strict), func(t *testing.T) {
				mask := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 8, []uint16{128}, jpeg2000.FormatJ2K))
				mask.Dict["Matte"] = types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
				base := jpegMatteStream("DeviceRGB", bytes.Repeat([]byte{255, 127, 127}, 64), false)
				base.Dict["SMask"] = mask
				switch kind {
				case "dimensions":
					base.Dict["Width"], mask.Dict["Width"] = types.Integer(4), types.Integer(4)
				case "components":
					mask.Raw = matteJPX(t, "DeviceRGB", 8, []uint16{128, 128, 128}, jpeg2000.FormatJ2K)
				case "SMaskInData":
					mask.Dict["SMaskInData"] = types.Integer(1)
				case "colourspace":
					mask.Dict["ColorSpace"] = types.Array{types.Name("CalGray"), types.Dict{}}
				case "missing raw colourspace":
					delete(mask.Dict, "ColorSpace")
				case "signed":
					// 裸码流的第一个 Ssiz 在固定 SIZ 段第 42 字节。
					mask.Raw = bytes.Clone(mask.Raw)
					mask.Raw[42] |= 0x80
				}
				var diagnostics []Diagnostic
				options, _ := (Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
				p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}, source: Diagnostic{Page: 1, Object: 5, Resource: "Im0"}}
				data, _, err := p.imageData(base, pdfColor{})
				if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_matte" || diagnostics[0].Object != 5 {
					t.Fatalf("diagnostics=%v", diagnostics)
				}
				if strict {
					if !errors.Is(err, ErrUnsupported) {
						t.Fatalf("strict error=%v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				img, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got != (color.NRGBA{255, 127, 127, 255}) {
					t.Fatalf("base changed: %v", got)
				}
			})
		}
	}
}

func matteJP2Box(kind string, payload []byte, extended bool) []byte {
	start := 8
	if extended {
		start = 16
	}
	data := make([]byte, start+len(payload))
	copy(data[4:8], kind)
	if extended {
		binary.BigEndian.PutUint32(data, 1)
		binary.BigEndian.PutUint64(data[8:16], uint64(len(data)))
	} else {
		binary.BigEndian.PutUint32(data, uint32(len(data)))
	}
	copy(data[start:], payload)
	return data
}

func TestPDFJPXMatteRejectsPaletteMappingICCAndMalformedBoxes(t *testing.T) {
	p := pdfInterpreter{}
	for _, kind := range []string{"pclr", "cmap", "cdef", "colr"} {
		data := matteJP2Box("jp2h", matteJP2Box(kind, []byte{2, 0, 0, 0, 0, 0, 0}, false), false)
		if err := p.checkMatteJPXBoxes(data, false); err == nil {
			t.Fatalf("accepted unsupported %s", kind)
		}
	}
	for _, data := range [][]byte{{0, 1}, {0, 0, 0, 1, 'f', 'r', 'e', 'e'}, {0, 0, 0, 7, 'f', 'r', 'e', 'e'}, {0xff, 0xff, 0xff, 0xff, 'f', 'r', 'e', 'e'}} {
		if err := p.checkMatteJPXBoxes(data, false); err == nil {
			t.Fatalf("accepted malformed box %x", data)
		}
	}
	valid := matteJP2Box("jp2h", matteJP2Box("colr", []byte{1, 0, 0, 0, 0, 0, 17}, true), true)
	if err := p.checkMatteJPXBoxes(valid, false); err != nil {
		t.Fatal(err)
	}
	zero := matteJP2Box("free", nil, false)
	binary.BigEndian.PutUint32(zero, 0)
	if err := p.checkMatteJPXBoxes(zero, false); err != nil {
		t.Fatal(err)
	}
}

func TestPDFJPXMatteReturnsCancellationWithoutWrapping(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := jpxMattePDF(t, "/Im0 Do", jpeg2000.FormatJP2)
	input = bytes.Replace(input, []byte("/Matte [1 1 1]"), []byte("/Matte [1]"), 1)
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancellation=%v output=%d", err, output.Len())
	}
}
