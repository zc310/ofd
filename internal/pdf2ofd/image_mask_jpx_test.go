package pdf2ofd

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPDFJPXColorKeyMatchesInclusiveOriginalComponents(t *testing.T) {
	for _, format := range []jpeg2000.Format{jpeg2000.FormatJP2, jpeg2000.FormatJ2K} {
		src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		pixels := []color.NRGBA{{10, 20, 30, 255}, {11, 21, 31, 255}, {10, 22, 30, 255}, {9, 20, 30, 255}}
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				src.SetNRGBA(x, y, pixels[x/2])
			}
		}
		stream := jpxMatteStream("DeviceRGB", encodeJPX(t, matteJPXRGB{src}, format))
		// JPX 按码流的 8 位原值匹配，字典位深和 Decode 都不改变区间。
		stream.Dict["BitsPerComponent"] = types.Integer(1)
		stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
		stream.Dict["Mask"] = types.Array{types.Integer(10), types.Integer(11), types.Integer(20), types.Integer(21), types.Integer(30), types.Integer(31)}
		if format == jpeg2000.FormatJP2 {
			delete(stream.Dict, "ColorSpace")
		}
		options, _ := (Options{Strict: true}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		img := jpxDecodedImage(t, &p, stream)
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				want := pixels[x/2]
				if x < 4 {
					want.A = 0
				}
				if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got != want {
					t.Fatalf("format=%v pixel %d,%d=%v want %v", format, x, y, got, want)
				}
			}
		}
	}
}

func TestPDFJPXColorKeyKeepsAdjacent16BitSamplesAndActualDimensions(t *testing.T) {
	src := image.NewGray16(image.Rect(0, 0, 2, 1))
	src.SetGray16(0, 0, color.Gray16{Y: 32768})
	src.SetGray16(1, 0, color.Gray16{Y: 32769})
	stream := jpxMatteStream("DeviceGray", encodeJPX(t, src, jpeg2000.FormatJP2))
	stream.Dict["Width"], stream.Dict["Height"], stream.Dict["BitsPerComponent"] = types.Integer(1), types.Integer(1), types.Integer(8)
	stream.Dict["Mask"] = types.Array{types.Integer(32768), types.Integer(32768)}
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if _, ok := img.(*image.NRGBA64); !ok || img.Bounds() != src.Bounds() {
		t.Fatalf("precision/dimensions lost: %T %v", img, img.Bounds())
	}
	for x, want := range []color.NRGBA64{{32768, 32768, 32768, 0}, {32769, 32769, 32769, 65535}} {
		if got := color.NRGBA64Model.Convert(img.At(x, 0)).(color.NRGBA64); got != want {
			t.Fatalf("pixel %d=%#v want %#v", x, got, want)
		}
	}
}

func TestPDFJPXColorKeyUsesCodestreamDepthFrom1To16Bits(t *testing.T) {
	for _, bits := range []int{1, 2, 4, 8, 9, 12, 16} {
		stream := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", bits, []uint16{0}, jpeg2000.FormatJ2K))
		stream.Dict["BitsPerComponent"] = types.Integer(8)
		stream.Dict["Mask"] = types.Array{types.Integer(0), types.Integer((1 << bits) - 1)}
		img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
		_, _, _, alpha := img.At(4, 4).RGBA()
		if alpha != 0 {
			t.Fatalf("%d bit key ignored: alpha=%d", bits, alpha)
		}
	}
}

func TestPDFJPXColorKeyPrecedesCMYKAndSYCCConversion(t *testing.T) {
	for _, tc := range []struct {
		name, cs string
		samples  []uint16
		roles    []int
		key      types.Array
	}{
		{"reordered RGB", "DeviceRGB", []uint16{30, 10, 20}, []int{3, 1, 2}, types.Array{types.Integer(10), types.Integer(10), types.Integer(20), types.Integer(20), types.Integer(30), types.Integer(30)}},
		{"CMYK raw ink", "DeviceCMYK", []uint16{0, 0, 0, 255}, nil, types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(255), types.Integer(255)}},
		{"reordered sYCC", "DeviceRGB", []uint16{210, 100, 170}, []int{3, 1, 2}, types.Array{types.Integer(100), types.Integer(100), types.Integer(170), types.Integer(170), types.Integer(210), types.Integer(210)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := matteJPX(t, tc.cs, 8, tc.samples, jpeg2000.FormatJP2)
			if tc.name == "reordered sYCC" {
				data = jpxWithColourEnum(t, data, 18)
			}
			if tc.roles != nil {
				data = reorderedJPX(t, data, 3, 0, tc.roles...)
			}
			stream := jpxMatteStream(tc.cs, data)
			stream.Dict["Mask"] = tc.key
			img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
			_, _, _, alpha := img.At(4, 4).RGBA()
			if alpha != 0 {
				t.Fatalf("matched converted colour instead of raw components: alpha=%d", alpha)
			}
		})
	}
}

func TestPDFJPXColorKeyReadsIndependentOpenJPEG16BitSamples(t *testing.T) {
	// OpenJPEG 2.5.0 的原灰度值是 32769；8 位显示与 32768 相同。
	data, err := base64.StdEncoding.DecodeString("AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAAAtanAyaAAAABZpaGRyAAAACAAAAAgAAQ8HAAAAAAAPY29scgEAAAAAABEAAACHanAyY/9P/1EAKQAAAAAACAAAAAgAAAAAAAAAAAAAAAgAAAAIAAAAAAAAAAAAAQ8BAf9SAAwAAAABAAAEBAAB/1wABECA/2QAJQABQ3JlYXRlZCBieSBPcGVuSlBFRyB2ZXJzaW9uIDIuNS4w/5AACgAAAAAAFQAB/5PAACQUAFyf/9k=")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []int{32768, 32769} {
		stream := jpxMatteStream("DeviceGray", data)
		stream.Dict["Mask"] = types.Array{types.Integer(key), types.Integer(key)}
		img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
		want := uint16(65535)
		if key == 32769 {
			want = 0
		}
		if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got.A != want || got.R != 32769 {
			t.Fatalf("independent key %d=%#v", key, got)
		}
	}
}

func TestPDFJPXColorKeyReadsIndirectArrayAndEndpoints(t *testing.T) {
	stream := jpxMatteStream("DeviceGray", matteJPX(t, "DeviceGray", 16, []uint16{32769}, jpeg2000.FormatJP2))
	stream.Dict["Mask"] = *types.NewIndirectRef(7, 0)
	xref := &model.XRefTable{Table: map[int]*model.XRefTableEntry{
		7: model.NewXRefTableEntryGen0(types.Array{*types.NewIndirectRef(8, 0), *types.NewIndirectRef(9, 0)}),
		8: model.NewXRefTableEntryGen0(types.Integer(32768)),
		9: model.NewXRefTableEntryGen0(types.Integer(32769)),
	}}
	p := pdfInterpreter{ctx: &model.Context{XRefTable: xref}}
	if got := color.NRGBA64Model.Convert(jpxDecodedImage(t, &p, stream).At(4, 4)).(color.NRGBA64); got.A != 0 || got.R != 32769 {
		t.Fatalf("indirect key=%#v", got)
	}
}

type cancelJPXColorKeyDuringComposition struct {
	context.Context

	control *conversionControl
	cancel  context.CancelFunc
	started bool
}

func (c *cancelJPXColorKeyDuringComposition) Err() error {
	// 两次三分量解码和首轮 PNG 输出之后，512 步已进入逐行遮罩合成。
	if c.control.operations >= 512 {
		c.started = true
		c.cancel()
	}
	return c.Context.Err()
}

func TestPDFJPXColorKeyStopsDuringCompositionWithoutWrappingCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{}).Normalize()
	control := &conversionControl{options: options}
	c := &cancelJPXColorKeyDuringComposition{Context: ctx, control: control, cancel: cancel}
	control.ctx = c
	p := pdfInterpreter{control: control}
	stream := jpxMatteStream("DeviceRGB", matteJPX(t, "DeviceRGB", 8, []uint16{255, 127, 127}, jpeg2000.FormatJP2))
	stream.Dict["Mask"] = types.Array{types.Integer(255), types.Integer(255), types.Integer(127), types.Integer(127), types.Integer(127), types.Integer(127)}
	if _, _, err := p.imageData(stream, pdfColor{}); err != context.Canceled || !c.started {
		t.Fatalf("cancel=%v composition started=%v", err, c.started)
	}
}

func jpxColorKeyObject(data []byte, extra string) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /Filter /JPXDecode %s /Length %d >>\nstream\n%s\nendstream", extra, len(data), data)
}

func TestPDFJPXColorKeyIncludesExtraDecodeBudgetAndCachesComposition(t *testing.T) {
	data := matteJPX(t, "DeviceRGB", 8, []uint16{255, 127, 127}, jpeg2000.FormatJP2)
	obj := jpxColorKeyObject(data, "/Mask [255 255 127 127 127 127]")
	input := budgetImagePDF("/Im0 Do", obj)
	// 基础解码、原分量二次解码、PNG 合成各计 64 像素。
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 127}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 191}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 450}, "operations")
	var output bytes.Buffer
	var diagnostics []Diagnostic
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", obj), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 192}, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%v", diagnostics)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got.A != 0 {
		t.Fatalf("cached key ignored: %v", got)
	}
}

func TestPDFJPXColorKeyYieldsToImageSoftMasks(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		pixel := color.NRGBA64{R: 65535, A: 128 * 257}
		if mode == 2 {
			pixel.R = pixel.A
		}
		stream := jpxAlphaStream(t, 8, mode, pixel)
		stream.Dict["Mask"] = types.Array{types.Integer(0), types.Integer(255), types.Integer(0), types.Integer(255), types.Integer(0), types.Integer(255)}
		img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
		want := byte(128)
		if mode == 0 {
			want = 0
		}
		if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got.A != want {
			t.Fatalf("mode %d=%v", mode, got)
		}
	}
	stream := jpxMatteStream("DeviceRGB", matteJPX(t, "DeviceRGB", 8, []uint16{255, 0, 0}, jpeg2000.FormatJP2))
	stream.Dict["Mask"] = types.Array{types.Integer(999)} // 被 SMask 覆盖的坏键不能触发诊断。
	stream.Dict["SMask"] = jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
	options, _ := (Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 192}}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	if got := color.NRGBAModel.Convert(jpxDecodedImage(t, &p, stream).At(4, 4)).(color.NRGBA); got != (color.NRGBA{255, 0, 0, 128}) {
		t.Fatalf("SMask priority=%v", got)
	}
}

func TestPDFJPXInvalidColorKeyReportsAndPreservesBase(t *testing.T) {
	data := matteJPX(t, "DeviceRGB", 8, []uint16{255, 127, 127}, jpeg2000.FormatJP2)
	for _, key := range []types.Array{
		{types.Integer(0), types.Integer(0)},
		{types.Integer(256), types.Integer(256), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
		{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
		{types.Float(.5), types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)},
	} {
		for _, strict := range []bool{false, true} {
			var diagnostics []Diagnostic
			options, _ := (Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
			stream := jpxMatteStream("DeviceRGB", data)
			stream.Dict["Mask"] = key
			encoded, _, err := p.imageData(stream, pdfColor{})
			if len(diagnostics) != 1 || diagnostics[0].Code != "image.mask_explicit" {
				t.Fatalf("key=%v diagnostics=%v", key, diagnostics)
			}
			if strict {
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("strict error=%v", err)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{255, 127, 127, 255}) {
				t.Fatalf("base lost: %v", got)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{OnDiagnostic: func(Diagnostic) { cancel() }}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: ctx, options: options}}
	stream := jpxMatteStream("DeviceRGB", data)
	stream.Dict["Mask"] = types.Array{types.Integer(0)}
	if _, _, err := p.imageData(stream, pdfColor{}); err != context.Canceled {
		t.Fatalf("cancel error=%v", err)
	}
}

func TestPDFJPXColorKeyStrictConversionRendersUnderlyingBackground(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			pixel := color.NRGBA{255, 255, 255, 255}
			if x >= 4 {
				pixel = color.NRGBA{255, 0, 0, 255}
			}
			src.SetNRGBA(x, y, pixel)
		}
	}
	obj := jpxColorKeyObject(encodeJPX(t, matteJPXRGB{src}, jpeg2000.FormatJP2), "/Mask [255 255 255 255 255 255]")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q", obj), &output, "", Options{Strict: true}); err != nil {
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
	for i, want := range []color.RGBA{{0, 0, 255, 255}, {255, 0, 0, 255}} {
		y := 100 * pdfPointToMillimeter * 4
		if got := img.RGBAAt(int(float64(60+i*80)*pdfPointToMillimeter*4), int(y)); got != want {
			t.Fatalf("block %d=%v want %v", i, got, want)
		}
	}
}
