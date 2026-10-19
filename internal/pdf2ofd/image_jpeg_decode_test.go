package pdf2ofd

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/pdf2ofd/config"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPDFOrdinaryJPEGDecodeTransformsOriginalComponents(t *testing.T) {
	for _, tc := range []struct {
		name, space string
		data        []byte
		decode      types.Array
		want        color.NRGBA
	}{
		{"gray inversion", "DeviceGray", matteJPEG(t, true, color.NRGBA{R: 0, A: 255}), types.Array{types.Integer(1), types.Integer(0)}, color.NRGBA{255, 255, 255, 255}},
		{"gray range", "DeviceGray", matteJPEG(t, true, color.NRGBA{R: 255, A: 255}), types.Array{types.Float(.25), types.Float(.75)}, color.NRGBA{191, 191, 191, 255}},
		{"RGB separate ranges", "DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 255, 255, 255}), types.Array{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(1), types.Integer(0), types.Float(.5)}, color.NRGBA{0, 255, 128, 255}},
		{"Adobe CMYK inversion", "DeviceCMYK", constantMatteCMYKJPEG([4]byte{0, 255, 255, 255}, 0), types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}, color.NRGBA{0, 173, 239, 255}},
		{"Adobe YCCK fixed zero ink", "DeviceCMYK", constantMatteCMYKJPEG([4]byte{127, 128, 128, 128}, 2), types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}, color.NRGBA{255, 255, 255, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := jpegMatteStream(tc.space, tc.data, true)
			stream.Dict["Decode"] = tc.decode
			options, _ := (Options{Strict: true}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
			img := jpxDecodedImage(t, &p, stream)
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got != tc.want {
						t.Fatalf("pixel %d,%d=%v want %v", x, y, got, tc.want)
					}
				}
			}
		})
	}
}

func TestPDFOrdinaryJPEGDecodeClampsAfterRangeMapping(t *testing.T) {
	stream := jpegMatteStream("DeviceGray", colorKeyJPEGHalves(t, true), true)
	stream.Dict["Width"] = types.Integer(16)
	stream.Dict["Decode"] = types.Array{types.Float(-.5), types.Float(1.5)}
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	for x := 0; x < 16; x++ {
		v := byte(0)
		if x >= 8 {
			v = 255
		}
		if got := color.NRGBAModel.Convert(img.At(x, 4)).(color.NRGBA); got != (color.NRGBA{v, v, v, 255}) {
			t.Fatalf("clamped pixel %d=%v", x, got)
		}
	}
}

func TestPDFOrdinaryJPEGIdentityDecodeKeepsOriginalBytes(t *testing.T) {
	for _, gray := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			space, components := "DeviceRGB", 3
			if gray {
				space, components = "DeviceGray", 1
			}
			data := matteJPEG(t, gray, color.NRGBA{64, 128, 192, 255})
			stream := jpegMatteStream(space, data, true)
			if explicit {
				decode := types.Array{}
				for c := 0; c < components; c++ {
					decode = append(decode, types.Integer(0), types.Integer(1))
				}
				stream.Dict["Decode"] = decode
			}
			options, _ := (Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 64, MaxOperations: 1}}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
			encoded, format, err := p.imageData(stream, pdfColor{})
			if err != nil || format != "JPEG" || !bytes.Equal(encoded, data) {
				t.Fatalf("gray=%v explicit=%v bytes changed: format=%s err=%v", gray, explicit, format, err)
			}
		}
	}
}

func TestPDFOrdinaryCMYKJPEGWithoutAdobeMarkerRespectsImageMode(t *testing.T) {
	data := constantMatteCMYKJPEG([4]byte{0, 255, 255, 255}, 0)
	segmentLength := int(data[4])<<8 | int(data[5])
	data = append(append([]byte(nil), data[:2]...), data[4+segmentLength:]...)
	for _, tc := range []struct {
		mode       config.ImageMode
		wantFormat string
	}{
		{config.ImageModeCompatible, "png"},
		{config.ImageModeCompact, "jpeg"},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			stream := jpegMatteStream("DeviceCMYK", data, true)
			options, err := (Options{ImageMode: tc.mode}).Normalize()
			if err != nil {
				t.Fatal(err)
			}
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
			encoded, format, err := p.imageData(stream, pdfColor{})
			if err != nil || format != map[string]string{"png": "PNG", "jpeg": "JPEG"}[tc.wantFormat] {
				t.Fatalf("format=%s err=%v", format, err)
			}
			var img image.Image
			if tc.wantFormat == "jpeg" {
				img, err = jpeg.Decode(bytes.NewReader(encoded))
			} else {
				img, err = png.Decode(bytes.NewReader(encoded))
			}
			if err != nil {
				t.Fatal(err)
			}
			wantR, wantG, wantB := pdfImageCMYKConverter(nil, stream).toRGB(0, 255, 255, 255)
			got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA)
			tolerance := uint8(0)
			if tc.mode == config.ImageModeCompact {
				tolerance = 12
			}
			if !nearColor(got.R, wantR, tolerance) || !nearColor(got.G, wantG, tolerance) || !nearColor(got.B, wantB, tolerance) || got.A != 255 {
				t.Fatalf("pixel=%v want RGB(%d,%d,%d) ±%d", got, wantR, wantG, wantB, tolerance)
			}
		})
	}
}

func nearColor(got, want, tolerance uint8) bool {
	delta := int(got) - int(want)
	if delta < 0 {
		delta = -delta
	}
	return delta <= int(tolerance)
}

func TestPDFCompactCMYKJPEGWithSoftMaskKeepsPNGAlpha(t *testing.T) {
	data := constantMatteCMYKJPEG([4]byte{0, 255, 255, 255}, 0)
	segmentLength := int(data[4])<<8 | int(data[5])
	data = append(append([]byte(nil), data[:2]...), data[4+segmentLength:]...)
	stream := jpegMatteStream("DeviceCMYK", data, true)
	stream.Dict["SMask"] = resolutionMask(8, 8, 8, bytes.Repeat([]byte{128}, 64))
	options, err := (Options{ImageMode: config.ImageModeCompact}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	encoded, format, err := p.imageData(stream, pdfColor{})
	if err != nil || format != "PNG" {
		t.Fatalf("format=%s err=%v", format, err)
	}
	img, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got.A < 120 || got.A > 136 {
		t.Fatalf("soft mask alpha=%d", got.A)
	}
}

func TestPDFOrdinaryJPEGDecodeBudgetsAndCache(t *testing.T) {
	data := matteJPEG(t, true, color.NRGBA{R: 0, A: 255})
	object := compressedColorKeyObject(data, "DCTDecode", "DeviceGray", "/Decode [1 0]", 8, 8, 8)
	input := budgetImagePDF("/Im0 Do", object)
	// 初次 JPEG 解码 64、PNG 合成 64；重复绘制命中缓存。
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 127}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 190}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", object), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 128, MaxOperations: 200}}); err != nil {
		t.Fatal(err)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got != (color.NRGBA{255, 255, 255, 255}) {
		t.Fatal(got)
	}
}

func TestPDFOrdinaryJPEGDecodeReadsFlateWrapperAndIndirectEndpoints(t *testing.T) {
	data := matteJPEG(t, true, color.NRGBA{R: 0, A: 255})
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stream := jpegMatteStream("DeviceGray", compressed.Bytes(), true)
	stream.Content = nil
	stream.FilterPipeline = []types.PDFFilter{{Name: "FlateDecode"}, {Name: "DCTDecode"}}
	stream.Dict["Decode"] = *types.NewIndirectRef(7, 0)
	stream.Dict["Width"], stream.Dict["Height"], stream.Dict["BitsPerComponent"] = *types.NewIndirectRef(10, 0), *types.NewIndirectRef(10, 0), *types.NewIndirectRef(10, 0)
	xref := &model.XRefTable{Table: map[int]*model.XRefTableEntry{
		7: model.NewXRefTableEntryGen0(types.Array{*types.NewIndirectRef(8, 0), *types.NewIndirectRef(9, 0)}),
		8: model.NewXRefTableEntryGen0(types.Integer(1)), 9: model.NewXRefTableEntryGen0(types.Integer(0)),
		10: model.NewXRefTableEntryGen0(types.Integer(8)),
	}}
	options, _ := (Options{Strict: true}).Normalize()
	p := pdfInterpreter{ctx: &model.Context{XRefTable: xref}, control: &conversionControl{ctx: t.Context(), options: options}}
	if got := color.NRGBAModel.Convert(jpxDecodedImage(t, &p, stream).At(4, 4)).(color.NRGBA); got != (color.NRGBA{255, 255, 255, 255}) {
		t.Fatal(got)
	}
	if reference, ok := stream.Dict["Decode"].(types.IndirectRef); !ok || reference.ObjectNumber != 7 {
		t.Fatal("Decode source dictionary changed")
	}
}

func TestPDFOrdinaryJPEGDecodeCombinesWithMasks(t *testing.T) {
	for _, kind := range []string{"SMask16", "explicit Mask", "color key"} {
		t.Run(kind, func(t *testing.T) {
			stream := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 0, A: 255}), true)
			stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
			want := uint16(65535)
			switch kind {
			case "SMask16":
				stream.Dict["SMask"] = resolutionMask(8, 8, 16, bytes.Repeat(softMaskSamples16(32769), 64))
				want = 32769
			case "explicit Mask":
				stream.Dict["Mask"] = &types.StreamDict{Dict: types.Dict{"Width": types.Integer(8), "Height": types.Integer(8), "BitsPerComponent": types.Integer(1), "ImageMask": types.Boolean(true)}, Content: make([]byte, 8)}
			case "color key":
				stream.Dict["Mask"] = types.Array{types.Integer(0), types.Integer(0)}
				want = 0
			}
			img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
			if source, ok := img.(*image.NRGBA64); ok {
				got := source.NRGBA64At(4, 4)
				if got != (color.NRGBA64{65535, 65535, 65535, want}) {
					t.Fatal(got)
				}
			} else {
				got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA)
				if got != (color.NRGBA{255, 255, 255, byte(want / 257)}) {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestPDFOrdinaryJPEGDecodeRejectsInvalidEndpoints(t *testing.T) {
	for _, decode := range []types.Object{
		types.Name("Invalid"), types.Array{types.Integer(1)},
		types.Array{types.Integer(0), types.Name("bad")},
		types.Array{types.Float(math.NaN()), types.Integer(0)},
		types.Array{types.Float(math.Inf(1)), types.Integer(0)},
	} {
		stream := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 0, A: 255}), true)
		stream.Dict["Decode"] = decode
		if _, _, err := (&pdfInterpreter{}).imageData(stream, pdfColor{}); err == nil {
			t.Fatalf("accepted invalid Decode %v", decode)
		}
	}
}

type cancelJPEGDecodeComposition struct {
	context.Context

	control *conversionControl
	cancel  context.CancelFunc
	entered bool
}

func (c *cancelJPEGDecodeComposition) Err() error {
	if c.control.operations >= 192 {
		c.entered = true
		c.cancel()
	}
	return c.Context.Err()
}

func TestPDFOrdinaryJPEGDecodeReturnsCancellationWithoutWrapping(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{}).Normalize()
	control := &conversionControl{options: options}
	c := &cancelJPEGDecodeComposition{Context: ctx, control: control, cancel: cancel}
	control.ctx = c
	stream := jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 0, A: 255}), true)
	stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
	p := pdfInterpreter{control: control}
	if _, _, err := p.imageData(stream, pdfColor{}); err != context.Canceled || !c.entered {
		t.Fatalf("cancel=%v composition=%v", err, c.entered)
	}
}

func TestPDFOrdinaryJPEGDecodeStrictConversionRendersInvertedImage(t *testing.T) {
	data := colorKeyJPEGHalves(t, true)
	object := compressedColorKeyObject(data, "DCTDecode", "DeviceGray", "/Decode [1 0]", 16, 8, 8)
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("q 160 0 0 80 20 60 cm /Im0 Do Q", object), &output, "", Options{Strict: true}); err != nil {
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
	for i, want := range []color.RGBA{{255, 255, 255, 255}, {0, 0, 0, 255}} {
		y := 100 * pdfPointToMillimeter * 4
		if got := img.RGBAAt(int(float64(60+i*80)*pdfPointToMillimeter*4), int(y)); got != want {
			t.Fatalf("block %d=%v want %v", i, got, want)
		}
	}
}

func TestPDFOrdinaryJPEGDecodeChecksDimensionsAndColorTransform(t *testing.T) {
	for _, failure := range []string{"dimensions", "ColorTransform"} {
		stream := jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{255, 255, 255, 255}), true)
		stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
		if failure == "dimensions" {
			stream.Dict["Width"] = types.Integer(16)
		} else {
			stream.FilterPipeline[0].DecodeParms = types.Dict{"ColorTransform": types.Integer(0)}
		}
		if _, _, err := (&pdfInterpreter{}).imageData(stream, pdfColor{}); err == nil {
			t.Fatalf("accepted JPEG %s", failure)
		}
	}
}

func TestPDFOrdinaryJPEGDecodeReportsUnsupportedTransformAndStrictFailure(t *testing.T) {
	data := matteJPEG(t, false, color.NRGBA{255, 255, 255, 255})
	object := compressedColorKeyObject(data, "DCTDecode", "DeviceRGB", "/Decode [1 0 1 0 1 0] /DecodeParms << /ColorTransform 0 >>", 8, 8, 8)
	input := budgetImagePDF("/Im0 Do", object)
	for _, strict := range []bool{false, true} {
		var output bytes.Buffer
		var diagnostics []Diagnostic
		err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if len(diagnostics) != 1 || diagnostics[0].Code != "image.decode" || diagnostics[0].Object != 5 || diagnostics[0].Page != 1 || diagnostics[0].Resource != "Im0" {
			t.Fatalf("diagnostics=%v", diagnostics)
		}
		if strict {
			if !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
				t.Fatalf("strict error=%v output=%d", err, output.Len())
			}
		} else if err != nil || output.Len() == 0 {
			t.Fatalf("normal error=%v output=%d", err, output.Len())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("diagnostic cancellation=%v output=%d", err, output.Len())
	}
}
