package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func separationTestStream(width, height, bits int, raw []byte) *types.StreamDict {
	// 红色专色的替代颜色为 (1, 1-t², 1-t²)，可区分灰度误判和求值前量化。
	space := types.Array{types.Name("Separation"), types.Name("SpotRed"), types.Name("DeviceRGB"), types.Dict{
		"FunctionType": types.Integer(2), "Domain": types.Array{types.Integer(0), types.Integer(1)},
		"C0": types.Array{types.Integer(1), types.Integer(1), types.Integer(1)},
		"C1": types.Array{types.Integer(1), types.Integer(0), types.Integer(0)}, "N": types.Integer(2),
	}}
	return &types.StreamDict{Dict: types.Dict{
		"Width": types.Integer(width), "Height": types.Integer(height),
		"BitsPerComponent": types.Integer(bits), "ColorSpace": space,
	}, Content: raw}
}

func TestSeparationImageAppliesDecodeBeforeTintAndKeepsPackedRows(t *testing.T) {
	for _, tc := range []struct {
		name                string
		width, height, bits int
		raw                 []byte
		decode              types.Array
		want                []uint8
	}{
		{"单比特行补齐", 3, 2, 1, []byte{0x5f, 0xbf}, nil, []uint8{255, 0, 255, 0, 255, 0}},
		{"双比特", 4, 1, 2, []byte{0x1b}, nil, []uint8{255, 226, 141, 0}},
		{"四比特", 3, 1, 4, []byte{0x08, 0xff}, nil, []uint8{255, 182, 0}},
		{"反相 Decode", 3, 1, 8, []byte{0, 128, 255}, types.Array{types.Integer(1), types.Integer(0)}, []uint8{0, 191, 255}},
		{"先限制 tint 范围", 3, 1, 8, []byte{0, 128, 255}, types.Array{types.Float(-1), types.Integer(2)}, []uint8{255, 189, 0}},
		{"16 位原采样求值", 1, 1, 16, []byte{0x80, 0x00}, nil, []uint8{191}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := separationTestStream(tc.width, tc.height, tc.bits, tc.raw)
			if tc.decode != nil {
				stream.Dict["Decode"] = tc.decode
			}
			data, format, err := pdfImageData(nil, stream, pdfColor{})
			if err != nil || format != "PNG" {
				t.Fatalf("format=%s error=%v", format, err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			for i, v := range tc.want {
				got := color.NRGBAModel.Convert(img.At(i%tc.width, i/tc.width)).(color.NRGBA)
				if want := (color.NRGBA{R: 255, G: v, B: v, A: 255}); got != want {
					t.Fatalf("pixel %d=%v want %v", i, got, want)
				}
			}
		})
	}
}

func TestSeparationImageColorKeyMatchesTintSamplesBeforeDecode(t *testing.T) {
	stream := separationTestStream(2, 1, 8, []byte{0, 255})
	stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0)}
	stream.Dict["Mask"] = types.Array{types.Integer(0), types.Integer(0)}
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("原始 tint=0 应被颜色键遮蔽")
	}
	if got := color.NRGBAModel.Convert(img.At(1, 0)).(color.NRGBA); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("反相后的 tint=0 应保留白色：%v", got)
	}
}

func TestSeparationImageHonoursBudgetsAndCancellation(t *testing.T) {
	for _, limits := range []Limits{{MaxImagePixels: 1}, {MaxTotalImagePixels: 1}, {MaxOperations: 1}} {
		options, _ := (Options{Limits: limits}).Normalize()
		p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
		if _, _, err := p.imageData(separationTestStream(2, 1, 8, []byte{0, 255}), pdfColor{}); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("limits=%+v error=%v", limits, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	options, _ := (Options{}).Normalize()
	p := pdfInterpreter{control: &conversionControl{ctx: ctx, options: options}}
	if _, _, err := p.imageData(separationTestStream(1, 1, 8, []byte{255}), pdfColor{}); err != context.Canceled {
		t.Fatalf("取消错误=%v", err)
	}
}

func TestSeparationImageRetainsImageSoftMask(t *testing.T) {
	stream := separationTestStream(2, 1, 8, []byte{255, 255})
	stream.Dict["SMask"] = &types.StreamDict{Dict: types.Dict{
		"Width": types.Integer(2), "Height": types.Integer(1),
		"BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray"),
	}, Content: []byte{0, 128}}
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
		t.Fatal("软掩码的透明像素应保留")
	}
	if got := color.NRGBAModel.Convert(img.At(1, 0)).(color.NRGBA); got != (color.NRGBA{R: 255, A: 128}) {
		t.Fatalf("专色与半透明 alpha 未保留：%v", got)
	}
}

func TestSeparationImageRejectsTruncatedSamplesAndInvalidTint(t *testing.T) {
	for _, invalidTint := range []bool{false, true} {
		stream := separationTestStream(2, 1, 8, []byte{255})
		if invalidTint {
			stream.Content = []byte{0, 255}
			stream.Dict["ColorSpace"].(types.Array)[3] = types.Dict{"FunctionType": types.Integer(99)}
		}
		if data, _, err := pdfImageData(nil, stream, pdfColor{}); err == nil || len(data) != 0 {
			t.Fatalf("invalidTint=%v output=%d error=%v", invalidTint, len(data), err)
		}
	}
}

func TestSeparationInlineImageStrictImportUsesTintComponentCount(t *testing.T) {
	content := "q 200 0 0 200 0 0 cm BI /W 2 /H 1 /BPC 8 /CS /Spot ID \x00\xff EI Q"
	input := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ColorSpace << /Spot [/Separation /Red /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [1 0 0] /N 1 >>] >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := render.NewDocument(color.White, ofd.Documents[0])
	defer document.Close()
	img, err := document.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPI(72))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []color.RGBA{{R: 255, G: 255, B: 255, A: 255}, {R: 255, A: 255}} {
		if got := img.RGBAAt(50+i*100, 100); got != want {
			t.Fatalf("区域 %d=%v want %v", i, got, want)
		}
	}
}
