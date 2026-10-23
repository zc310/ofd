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
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

// matteJBIG2Page 用页面信息段的默认像素生成独立的常量二值图，不依赖外部
// 夹具或编码器。默认位 1 表示墨，gobig2 的 Gray 输出则是黑 0、白 255。
func matteJBIG2Page(width, height int, black bool) []byte {
	data := make([]byte, 30)
	data[4], data[6], data[10] = 48, 1, 19
	binary.BigEndian.PutUint32(data[11:15], uint32(width))
	binary.BigEndian.PutUint32(data[15:19], uint32(height))
	if black {
		data[27] = 4
	}
	return data
}

func jbig2MatteStream(black bool) *types.StreamDict {
	return &types.StreamDict{
		Dict: types.Dict{"Width": types.Integer(8), "Height": types.Integer(8), "BitsPerComponent": types.Integer(1), "ColorSpace": types.Name("DeviceGray")},
		Raw:  matteJBIG2Page(8, 8, black), FilterPipeline: []types.PDFFilter{{Name: "JBIG2Decode"}},
	}
}

func TestPDFJBIG2MatteAppliesBaseAndMaskDecodeBeforeUnblending(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		baseJBIG2, maskJBIG2   bool
		baseBlack, maskBlack   bool
		baseDecode, maskDecode types.Array
		alpha                  byte
	}{
		{"JBIG2 base", true, false, false, false, types.Array{types.Integer(0), types.Float(.5)}, nil, 128},
		{"JBIG2 mask white endpoint", false, true, false, false, nil, types.Array{types.Integer(0), types.Float(.5)}, 128},
		{"JBIG2 mask black endpoint", false, true, false, true, nil, types.Array{types.Float(.5), types.Integer(1)}, 128},
		{"both JBIG2 and Decode", true, true, true, false, types.Array{types.Float(.5), types.Integer(1)}, types.Array{types.Integer(0), types.Float(.5)}, 128},
		{"mask transparent", false, true, false, true, nil, nil, 0},
		{"mask opaque", false, true, false, false, nil, nil, 255},
		{"mask inverted", false, true, false, true, nil, types.Array{types.Integer(1), types.Integer(0)}, 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
			mask := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
			if tc.baseJBIG2 {
				base = jbig2MatteStream(tc.baseBlack)
			}
			if tc.maskJBIG2 {
				mask = jbig2MatteStream(tc.maskBlack)
			}
			base.Dict["SMask"] = mask
			mask.Dict["Matte"] = types.Array{types.Integer(0)}
			if tc.baseDecode != nil {
				base.Dict["Decode"] = tc.baseDecode
			}
			if tc.maskDecode != nil {
				mask.Dict["Decode"] = tc.maskDecode
			}
			options, _ := (Options{Strict: true}).Normalize()
			p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
			data, format, err := p.imageData(base, pdfColor{})
			if err != nil || format != "PNG" {
				t.Fatalf("format=%s error=%v", format, err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			want := color.NRGBA{R: 255, G: 255, B: 255, A: tc.alpha}
			if tc.baseJBIG2 && !tc.maskJBIG2 {
				// 基础 Decode 得到精确 .5，普通 8 位掩码则为 128/255。
				want.R, want.G, want.B = 254, 254, 254
			}
			if tc.alpha == 0 {
				want = color.NRGBA{}
			} else if tc.alpha == 255 {
				want = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
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

func TestPDFJBIG2MatteMaskPreserves16BitDecodeEndpoints(t *testing.T) {
	mask := jbig2MatteStream(false)
	mask.Dict["Matte"] = types.Array{types.Integer(0), types.Integer(0), types.Integer(0)}
	mask.Dict["Decode"] = types.Array{types.Integer(0), types.Float(float64(32769) / 65535)}
	base := jpegMatteStream("DeviceRGB", bytes.Repeat(softMaskSamples16(32769, 0, 0), 64), false)
	base.Dict["BitsPerComponent"], base.Dict["SMask"] = types.Integer(16), mask
	data, _, err := pdfImageData(nil, base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.NRGBA64); !ok {
		t.Fatalf("16-bit output lost: %T", img)
	}
	if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: 65535, A: 32769}) {
		t.Fatalf("Decode precision lost: %#v", got)
	}
}

func TestPDFJBIG2MatteSupportsJPEGAndCMYKInputs(t *testing.T) {
	for _, kind := range []string{"JPEG RGB base", "CMYK base", "JPEG mask"} {
		t.Run(kind, func(t *testing.T) {
			mask := jbig2MatteStream(false)
			mask.Dict["Decode"] = types.Array{types.Integer(0), types.Float(.5)}
			mask.Dict["Matte"] = types.Array{types.Integer(0), types.Integer(0), types.Integer(0)}
			base := jpegMatteStream("DeviceRGB", matteJPEG(t, false, color.NRGBA{R: 128, A: 255}), true)
			want := color.NRGBA{R: 255, A: 128}
			switch kind {
			case "CMYK base":
				base = jpegMatteStream("DeviceCMYK", bytes.Repeat([]byte{128, 0, 0, 0}, 64), false)
				mask.Dict["Matte"] = types.Array{types.Integer(0), types.Integer(0), types.Integer(0), types.Integer(0)}
				want.R, want.G, want.B = cmykInkToRGB(255, 0, 0, 0)
			case "JPEG mask":
				base = jbig2MatteStream(false)
				base.Dict["Decode"] = types.Array{types.Integer(0), types.Float(float64(128) / 255)}
				mask = jpegMatteStream("DeviceGray", matteJPEG(t, true, color.NRGBA{R: 128, A: 255}), true)
				mask.Dict["Matte"] = types.Array{types.Integer(0)}
				want.G, want.B = 255, 255
			}
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
			got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA)
			if kind == "JPEG RGB base" {
				if got.R < 250 || got.G > 5 || got.B > 5 || got.A != 128 {
					t.Fatalf("JPEG pixel=%v", got)
				}
			} else if got != want {
				t.Fatalf("pixel=%v want %v", got, want)
			}
		})
	}
}

func TestPDFJBIG2MatteReadsBoundedCompressedGlobalsAndIndirectDecode(t *testing.T) {
	// 一个无符号字典的全局段流，以 EOF 结束。验证查找、Flate 解码及预算，
	// 不用外部夹具；符号字典本身仍由既有的 gobig2 全局段解码器处理。
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
	makeStreams := func() (*types.StreamDict, *types.StreamDict, *model.Context) {
		gs := types.StreamDict{Dict: types.Dict{}, Raw: compressed.Bytes(), FilterPipeline: []types.PDFFilter{{Name: "FlateDecode"}}}
		xref := &model.XRefTable{Table: map[int]*model.XRefTableEntry{
			7: model.NewXRefTableEntryGen0(gs),
			8: model.NewXRefTableEntryGen0(types.Float(.5)),
		}}
		mask := jbig2MatteStream(false)
		mask.FilterPipeline[0].DecodeParms = types.Dict{"JBIG2Globals": *types.NewIndirectRef(7, 0)}
		mask.Dict["Decode"] = types.Array{types.Integer(0), *types.NewIndirectRef(8, 0)}
		mask.Dict["Matte"] = types.Array{types.Integer(0)}
		base := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
		base.Dict["SMask"] = mask
		return base, mask, &model.Context{XRefTable: xref}
	}
	base, _, ctx := makeStreams()
	options, _ := (Options{Strict: true, Limits: Limits{MaxTotalDecodedBytes: 105}}).Normalize()
	p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	data, _, err := p.imageData(base, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 128}) {
		t.Fatalf("globals pixel=%v", got)
	}
	if p.control.decoded != 105 {
		t.Fatalf("decoded bytes=%d want 105", p.control.decoded)
	}
	base, _, ctx = makeStreams()
	options, _ = (Options{Limits: Limits{MaxTotalDecodedBytes: 104}}).Normalize()
	p = pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}}
	if _, _, err := p.imageData(base, pdfColor{}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("globals budget=%v", err)
	}
}

func jbig2MattePDF(content string) []byte {
	base := bytes.Repeat([]byte{255, 127, 127}, 64)
	mask := matteJBIG2Page(8, 8, true)
	return budgetImagePDF(content,
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R /Length %d >>\nstream\n%s\nendstream", len(base), base),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 1 /Matte [1 1 1] /Decode [.5 1] /Filter /JBIG2Decode /Length %d >>\nstream\n%s\nendstream", len(mask), mask))
}

func TestPDFJBIG2MatteStrictConversionRendersWithoutFringe(t *testing.T) {
	var output bytes.Buffer
	input := jbig2MattePDF("0 0 1 rg 0 0 200 200 re f q 160 0 0 80 20 60 cm /Im0 Do Q")
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
	if got := img.RGBAAt(int(x), int(y)); got.R < 126 || got.R > 130 || got.G > 2 || got.B < 125 || got.B > 129 {
		t.Fatalf("white fringe: %v", got)
	}
}

func TestPDFJBIG2MatteBudgetsIncludeDecodeAndCache(t *testing.T) {
	input := jbig2MattePDF("/Im0 Do")
	// 基础图、掩码、合成与额外 JBIG2 解码各 64 像素。
	requireBudgetError(t, input, Limits{MaxTotalImagePixels: 255}, "total_image_pixels")
	requireBudgetError(t, input, Limits{MaxOperations: 385}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), jbig2MattePDF("/Im0 Do /Im0 Do"), &output, "", Options{Strict: true, Limits: Limits{MaxTotalImagePixels: 256, MaxOperations: 388}}); err != nil {
		t.Fatal(err)
	}
	mask := jbig2MatteStream(false)
	mask.Dict["Width"], mask.Dict["Height"] = types.Integer(1), types.Integer(1)
	options, _ := (Options{Limits: Limits{MaxImagePixels: 4}}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	if err := p.preflightImage(mask, 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("JBIG2 actual size budget=%v", err)
	}
}

func TestPDFJBIG2MatteReportsInvalidMaskAndKeepsBase(t *testing.T) {
	for _, kind := range []string{"dimensions", "bits", "truncated"} {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_strict%v", kind, strict), func(t *testing.T) {
				var diagnostics []Diagnostic
				options, _ := (Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
				p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}, source: Diagnostic{Page: 1, Object: 5, Resource: "Im0"}}
				mask := jbig2MatteStream(true)
				mask.Dict["Matte"] = types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
				base := jpegMatteStream("DeviceRGB", bytes.Repeat([]byte{255, 127, 127}, 64), false)
				base.Dict["SMask"] = mask
				switch kind {
				case "dimensions":
					mask.Raw = matteJBIG2Page(4, 8, true)
				case "bits":
					mask.Dict["BitsPerComponent"] = types.Integer(8)
				case "truncated":
					mask.Raw = mask.Raw[:20]
				}
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
				if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 255, G: 127, B: 127, A: 255}) {
					t.Fatalf("base changed: %v", got)
				}
			})
		}
	}
}

func TestPDFJBIG2MatteReturnsCancellationWithoutWrapping(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options, _ := (Options{}).Normalize()
	control := &conversionControl{ctx: ctx, options: options}
	cancel()
	if _, err := pdfJBIG2Image(nil, jbig2MatteStream(true), control); err != context.Canceled {
		t.Fatalf("JBIG2 cancellation=%v", err)
	}
	var output bytes.Buffer
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	input := bytes.Replace(jbig2MattePDF("/Im0 Do"), []byte("/Matte [1 1 1]"), []byte("/Matte [1]"), 1)
	err := ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("callback cancellation=%v output=%d", err, output.Len())
	}
}

// 在解码器检查段间取消时触发，保护 DecodeContext 的传递和原样返回语义；
// 若只在解码前后检查，两次 Err 调用不足以触发此取消。
type cancelJBIG2DuringDecode struct {
	context.Context

	cancel context.CancelFunc
	checks int
}

func (c *cancelJBIG2DuringDecode) Err() error {
	c.checks++
	if c.checks == 3 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestPDFJBIG2MatteStopsDecoderBetweenSegments(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &cancelJBIG2DuringDecode{Context: ctx, cancel: cancel}
	options, _ := (Options{}).Normalize()
	control := &conversionControl{ctx: c, options: options}
	if _, err := pdfJBIG2Image(nil, jbig2MatteStream(false), control); err != context.Canceled {
		t.Fatalf("during decode cancellation=%v checks=%d", err, c.checks)
	}
}
