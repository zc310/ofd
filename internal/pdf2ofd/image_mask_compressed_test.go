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
	"image/jpeg"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

// Independent JBIG2: white page plus an immediate MMR generic region.
// First row H(001), white run 4(1011), black run 4(011); seven identical
// rows use V0,V0 (11). Packed MMR bytes are 36 ff ff: left white, right black.
func colorKeyJBIG2Halves() []byte {
	data := matteJBIG2Page(8, 8, false)
	segment := make([]byte, 32)
	binary.BigEndian.PutUint32(segment, 1)
	segment[4], segment[6] = 38, 1
	binary.BigEndian.PutUint32(segment[7:], 21)
	binary.BigEndian.PutUint32(segment[11:], 8)
	binary.BigEndian.PutUint32(segment[15:], 8)
	segment[28] = 1
	copy(segment[29:], []byte{0x36, 0xff, 0xff})
	return append(data, segment...)
}

func colorKeyJPEGHalves(t *testing.T, gray bool) []byte {
	t.Helper()
	var src image.Image
	if gray {
		g := image.NewGray(image.Rect(0, 0, 16, 8))
		for y := 0; y < 8; y++ {
			for x := 8; x < 16; x++ {
				g.SetGray(x, y, color.Gray{Y: 255})
			}
		}
		src = g
	} else {
		rgb := image.NewNRGBA(image.Rect(0, 0, 16, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 16; x++ {
				v := color.NRGBA{255, 255, 255, 255}
				if x >= 8 {
					v = color.NRGBA{255, 0, 0, 255}
				}
				rgb.SetNRGBA(x, y, v)
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

func TestPDFJPEGColorKeyMatchesBeforeDecodeAndAllComponents(t *testing.T) {
	for _, gray := range []bool{true, false} {
		family := "DeviceRGB"
		key := types.Array{types.Integer(254), types.Integer(255), types.Integer(254), types.Integer(255), types.Integer(254), types.Integer(255)}
		if gray {
			family = "DeviceGray"
			key = types.Array{types.Integer(0), types.Integer(0)}
		}
		data := colorKeyJPEGHalves(t, gray)
		stream := jpegMatteStream(family, data, true)
		stream.Dict["Width"] = types.Integer(16)
		stream.Dict["Mask"] = key
		if gray {
			stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
		}
		options, _ := (Options{Strict: true}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		img := jpxDecodedImage(t, &p, stream)
		if img.Bounds() != image.Rect(0, 0, 16, 8) {
			t.Fatal(img.Bounds())
		}
		for y := 0; y < 8; y++ {
			for x := 0; x < 16; x++ {
				got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if (got.A == 0) != (x < 8) {
					t.Fatalf("gray=%v pixel %d,%d key failed: %v", gray, x, y, got)
				}
				if x >= 8 && gray && (got.R != 0 || got.G != 0 || got.B != 0) {
					t.Fatalf("gray Decode not applied: %v", got)
				}
				if x >= 8 && !gray && (got.R < 250 || got.G > 3 || got.B > 3) {
					t.Fatalf("visible RGB changed: %v", got)
				}
			}
		}
	}
}

func TestPDFJPEGColorKeyMatchesAdobeCMYKAndYCCKOriginalSamples(t *testing.T) {
	for _, tc := range []struct {
		name      string
		samples   [4]byte
		transform byte
		raw       [4]byte
	}{
		{"CMYK", [4]byte{0, 128, 64, 255}, 0, [4]byte{0, 128, 64, 255}},
		{"YCCK", [4]byte{127, 128, 128, 128}, 2, [4]byte{128, 128, 128, 128}},
	} {
		for _, matches := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_match%v", tc.name, matches), func(t *testing.T) {
				stream := jpegMatteStream("DeviceCMYK", constantMatteCMYKJPEG(tc.samples, tc.transform), true)
				key := types.Array{}
				for c, v := range tc.raw {
					if !matches && c == 3 {
						v--
					}
					key = append(key, types.Integer(v), types.Integer(v))
				}
				stream.Dict["Mask"], stream.Dict["Decode"] = key, types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
				img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
				got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA)
				if (got.A == 0) != matches {
					t.Fatalf("raw Adobe samples key mismatch: %v", got)
				}
				r, g, b := pdfImageCMYKConverter(nil, stream).toRGB(255-tc.raw[0], 255-tc.raw[1], 255-tc.raw[2], 255-tc.raw[3])
				if got.R != r || got.G != g || got.B != b {
					t.Fatalf("Decode/color conversion=%v want %d,%d,%d", got, r, g, b)
				}
			})
		}
	}
}

func TestPDFJBIG2ColorKeyUsesBinarySamplesBeforeDecode(t *testing.T) {
	for _, invert := range []bool{false, true} {
		for _, key := range []int{0, 1} {
			stream := jbig2MatteStream(false)
			stream.Raw = colorKeyJBIG2Halves()
			stream.Dict["Mask"] = types.Array{types.Integer(key), types.Integer(key)}
			if invert {
				stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
			}
			img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					raw := 0
					if x < 4 {
						raw = 1
					}
					v := byte(255 * raw)
					if invert {
						v = 255 - v
					}
					alpha := byte(255)
					if raw == key {
						alpha = 0
					}
					want := color.NRGBA{v, v, v, alpha}
					if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got != want {
						t.Fatalf("invert=%v key=%d pixel %d,%d=%v want %v", invert, key, x, y, got, want)
					}
				}
			}
		}
	}
}

func compressedColorKeyObject(data []byte, kind, space, extra string, width, height, bits int) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /BitsPerComponent %d /ColorSpace /%s /Filter /%s %s /Length %d >>\nstream\n%s\nendstream", width, height, bits, space, kind, extra, len(data), data)
}

func TestPDFCompressedColorKeyIncludesDecodeAndCompositionBudgetAndCache(t *testing.T) {
	for _, kind := range []string{"DCTDecode", "JBIG2Decode"} {
		data := matteJPEG(t, true, color.NRGBA{R: 0, A: 255})
		bits := 8
		if kind == "JBIG2Decode" {
			data = colorKeyJBIG2Halves()
			bits = 1
		}
		object := compressedColorKeyObject(data, kind, "DeviceGray", "/Mask [0 0]", 8, 8, bits)
		input := budgetImagePDF("/Im0 Do", object)
		requireBudgetError(t, input, Limits{MaxTotalImagePixels: 127}, "total_image_pixels")
		requireBudgetError(t, input, Limits{MaxTotalImagePixels: 191}, "total_image_pixels")
		requireBudgetError(t, input, Limits{MaxOperations: 200}, "operations")
		var output bytes.Buffer
		if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", object), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 192}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPDFJPEGColorKeySupportsFlateWrapperAndIndirectKey(t *testing.T) {
	data := matteJPEG(t, true, color.NRGBA{R: 0, A: 255})
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	object := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /BitsPerComponent 8 /ColorSpace /DeviceGray /Filter [/FlateDecode /DCTDecode] /Mask 6 0 R /Length %d >>\nstream\n%s\nendstream", compressed.Len(), compressed.Bytes())
	input := assemblePDF([]string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 5 0 R >> >> /Contents 4 0 R >>", "<< /Length 7 >>\nstream\n/Im0 Do\nendstream", object, "[7 0 R 8 0 R]", "0", "0"})
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got.A != 0 {
		t.Fatalf("wrapped JPEG key=%v", got)
	}
}

func TestPDFJBIG2ColorKeyUsesCompressedGlobals(t *testing.T) {
	globals := make([]byte, 11)
	globals[4] = 51
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(globals); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stream := jbig2MatteStream(false)
	stream.Raw = colorKeyJBIG2Halves()
	stream.Dict["Mask"] = types.Array{types.Integer(1), types.Integer(1)}
	stream.FilterPipeline[0].DecodeParms = types.Dict{"JBIG2Globals": *types.NewIndirectRef(7, 0)}
	xref := &model.XRefTable{Table: map[int]*model.XRefTableEntry{7: model.NewXRefTableEntryGen0(types.StreamDict{Dict: types.Dict{}, Raw: compressed.Bytes(), FilterPipeline: []types.PDFFilter{{Name: "FlateDecode"}}})}}
	options, _ := (Options{Strict: true}).Normalize()
	p := pdfInterpreter{ctx: &model.Context{XRefTable: xref}, control: &conversionControl{ctx: t.Context(), options: options}}
	img := jpxDecodedImage(t, &p, stream)
	if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got.A != 0 {
		t.Fatalf("global-backed JBIG2 key=%v", got)
	}
}

func TestPDFCompressedColorKeyYieldsToSMask(t *testing.T) {
	for _, kind := range []string{"JPEG", "JBIG2"} {
		base := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 0, A: 255}), true)
		if kind == "JBIG2" {
			base = jbig2MatteStream(true)
		}
		base.Dict["Mask"] = types.Array{types.Integer(999)}
		base.Dict["SMask"] = jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
		options, _ := (Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 192}}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		if got := color.NRGBAModel.Convert(jpxDecodedImage(t, &p, base).At(4, 4)).(color.NRGBA); got.A != 128 {
			t.Fatalf("%s SMask priority=%v", kind, got)
		}
	}
}

func TestPDFCompressedColorKeyInvalidDataReportsAndPreservesBase(t *testing.T) {
	for _, kind := range []string{"JPEG", "JBIG2"} {
		for _, failure := range []string{"reversed range", "overflow range", "actual dimensions", "ColorTransform"} {
			if kind == "JBIG2" && failure == "ColorTransform" {
				continue
			}
			makeStream := func() *types.StreamDict {
				base := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 0, A: 255}), true)
				if kind == "JBIG2" {
					base = jbig2MatteStream(true)
				}
				base.Dict["Mask"] = types.Array{types.Integer(1), types.Integer(0)}
				switch failure {
				case "overflow range":
					base.Dict["Mask"] = types.Array{types.Integer(256), types.Integer(256)}
				case "actual dimensions":
					base.Dict["Width"] = types.Integer(16)
					base.Dict["Mask"] = types.Array{types.Integer(0), types.Integer(0)}
				case "ColorTransform":
					base = jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 0, 0, 255}), true)
					base.Dict["Mask"] = types.Array{types.Integer(255), types.Integer(255), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}
					base.FilterPipeline[0].DecodeParms = types.Dict{"ColorTransform": types.Integer(0)}
				}
				return base
			}
			for _, strict := range []bool{false, true} {
				var diagnostics []Diagnostic
				options, _ := (Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
				p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
				data, _, err := p.imageData(makeStream(), pdfColor{})
				if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_explicit" {
					t.Fatalf("%s %s diagnostics=%v", kind, failure, diagnostics)
				}
				if strict {
					if !errors.Is(err, ErrUnsupported) {
						t.Fatal(err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				img, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, alpha := img.At(4, 4).RGBA()
				if alpha != 65535 {
					t.Fatal("failed key removed base image")
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			options, _ := (Options{OnDiagnostic: func(Diagnostic) { cancel() }}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: ctx, options: options}}
			if _, _, err := p.imageData(makeStream(), pdfColor{}); err != context.Canceled {
				t.Fatalf("callback cancellation=%v", err)
			}
			cancel()
		}
	}
}

type cancelCompressedKeySampling struct {
	context.Context

	control *conversionControl
	cancel  context.CancelFunc
	entered bool
}

func (c *cancelCompressedKeySampling) Err() error {
	if c.control.operations >= 256 {
		c.entered = true
		c.cancel()
	}
	return c.Context.Err()
}

func TestPDFCompressedColorKeyReturnsCancellationDuringComposition(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{}).Normalize()
	control := &conversionControl{options: options}
	c := &cancelCompressedKeySampling{Context: ctx, control: control, cancel: cancel}
	control.ctx = c
	p := pdfInterpreter{control: control}
	base := jbig2MatteStream(true)
	base.Dict["Mask"] = types.Array{types.Integer(0), types.Integer(0)}
	if _, _, err := p.imageData(base, pdfColor{}); err != context.Canceled || !c.entered {
		t.Fatalf("composition cancellation=%v entered=%v", err, c.entered)
	}
}

func TestPDFCompressedColorKeyStrictConversionRendersSourceBackground(t *testing.T) {
	for _, kind := range []string{"DCTDecode", "JBIG2Decode"} {
		data, width, bits, space, extra := colorKeyJPEGHalves(t, true), 16, 8, "DeviceGray", "/Mask [0 0] /Decode [1 0]"
		if kind == "JBIG2Decode" {
			data, width, bits, extra = colorKeyJBIG2Halves(), 8, 1, "/Mask [1 1]"
		}
		content := "0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q"
		var output bytes.Buffer
		if err := ConvertWithOptions(t.Context(), budgetImagePDF(content, compressedColorKeyObject(data, kind, space, extra, width, 8, bits)), &output, "", Options{Strict: true}); err != nil {
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
		for i, want := range []color.RGBA{{0, 0, 255, 255}, {0, 0, 0, 255}} {
			y := 100 * pdfPointToMillimeter * 4
			if got := img.RGBAAt(int(float64(60+i*80)*pdfPointToMillimeter*4), int(y)); got != want {
				t.Fatalf("%s block %d=%v want %v", kind, i, got, want)
			}
		}
	}
}
