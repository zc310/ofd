package pdf2ofd

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// associations 按原平面次序声明颜色角色；0 为作用于所有颜色的 opacity。
func reorderedJPX(t *testing.T, data []byte, colours, mode int, associations ...int) []byte {
	t.Helper()
	data = jpxWithChannelDefinitions(t, data, colours, len(associations), mode)
	pos := bytes.Index(data, []byte("cdef")) + 4
	for channel, association := range associations {
		entry := data[pos+2+channel*6:]
		kind := 0
		if association == 0 {
			kind = mode
			if kind == 0 {
				kind = 1
			}
		}
		binary.BigEndian.PutUint16(entry[2:], uint16(kind))
		binary.BigEndian.PutUint16(entry[4:], uint16(association))
	}
	return data
}

func TestPDFJPXReordersRGBAndCMYKBeforeColourConversion(t *testing.T) {
	for _, bits := range []int{8, 16} {
		for _, cs := range []string{"DeviceRGB", "DeviceCMYK"} {
			t.Run(fmt.Sprintf("%s_%d", cs, bits), func(t *testing.T) {
				samples := []uint16{18, 52, 86}
				associations := []int{3, 1, 2}
				if cs == "DeviceCMYK" {
					samples = append(samples, 120)
					associations = []int{4, 2, 1, 3}
				}
				if bits == 16 && cs == "DeviceCMYK" {
					samples = []uint16{0x1234, 0x3456, 0x5678, 0x789a}
				}
				stream := jpxMatteStream(cs, reorderedJPX(t, matteJPX(t, cs, bits, samples, jpeg2000.FormatJP2), len(samples), 0, associations...))
				img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
				var values [4]uint16
				for plane, association := range associations {
					value := samples[plane]
					// 三分量测试编码器自然精度为 8 位，16 位编码按此缩放。
					if bits == 8 || cs == "DeviceRGB" {
						value *= 257
					}
					values[association-1] = value
				}
				r, g, b := values[0], values[1], values[2]
				if cs == "DeviceCMYK" {
					converter := pdfImageCMYKConverter(nil, stream)
					if bits == 16 {
						r, g, b = converter.toRGB16(values[0], values[1], values[2], values[3])
					} else {
						r8, g8, b8 := converter.toRGB(byte(values[0]/257), byte(values[1]/257), byte(values[2]/257), byte(values[3]/257))
						r, g, b = uint16(r8)*257, uint16(g8)*257, uint16(b8)*257
					}
				}
				if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: r, G: g, B: b, A: 65535}) {
					t.Fatalf("reordered pixel=%#v want %x %x %x", got, r, g, b)
				}
			})
		}
	}
}

func TestPDFJPXOpacityCanPrecedeColourPlanes(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		pixel := color.NRGBA64{R: 32769, G: 65535, B: 0, A: 0}
		if mode == 2 {
			pixel.G = 32769
		}
		stream := jpxAlphaStream(t, 16, mode, pixel)
		stream.Raw = reorderedJPX(t, stream.Raw, 3, mode, 0, 1, 2, 3)
		img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
		want := color.NRGBA64{R: 65535, A: 32769}
		if mode == 0 {
			want.A = 65535
		}
		if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != want {
			t.Fatalf("mode %d=%#v want %#v", mode, got, want)
		}
	}
}

func TestPDFJPXReordersBeforeMatteColourRecovery(t *testing.T) {
	stream := jpxMatteStream("DeviceRGB", reorderedJPX(t, matteJPX(t, "DeviceRGB", 8, []uint16{127, 255, 127}, jpeg2000.FormatJP2), 3, 0, 3, 1, 2))
	mask := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
	mask.Dict["Matte"] = types.Array{types.Integer(1), types.Integer(1), types.Integer(1)}
	stream.Dict["SMask"] = mask
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 255, A: 128}) {
		t.Fatalf("Matte used wrong channel order: %v", got)
	}
}

func TestPDFJPXIndependentOpenJPEGColourAssociation(t *testing.T) {
	// 独立 OpenJPEG 三分量 RGB 样本 (255,127,127)，仅增加通道关联，
	// 逻辑次序为 G,B,R，预期 (127,127,255)，不依赖 Go 编码器往返。
	data, err := base64.StdEncoding.DecodeString("AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAAAtanAyaAAAABZpaGRyAAAACAAAAAgAAwcHAAAAAAAPY29scgEAAAAAABAAAACXanAyY/9P/1EALwAAAAAACAAAAAgAAAAAAAAAAAAAAAgAAAAIAAAAAAAAAAAAAwcBAQcBAQcBAf9SAAwAAAABAQAEBAAB/1wABEBA/2QAJQABQ3JlYXRlZCBieSBPcGVuSlBFRyB2ZXJzaW9uIDIuNS4w/5AACgAAAAAAHwAB/5PD5woUAFyvh4DfgCgUAFyjb//Z")
	if err != nil {
		t.Fatal(err)
	}
	stream := jpxMatteStream("DeviceRGB", reorderedJPX(t, data, 3, 0, 3, 1, 2))
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 127, G: 127, B: 255, A: 255}) {
		t.Fatalf("independent colour association=%v", got)
	}
}

func TestPDFJPXChannelDefinitionEntryOrderDoesNotChangeMapping(t *testing.T) {
	stream := jpxMatteStream("DeviceRGB", reorderedJPX(t, matteJPX(t, "DeviceRGB", 8, []uint16{18, 52, 86}, jpeg2000.FormatJP2), 3, 0, 3, 1, 2))
	pos := bytes.Index(stream.Raw, []byte("cdef")) + 6
	first := bytes.Clone(stream.Raw[pos : pos+6])
	copy(stream.Raw[pos:pos+6], stream.Raw[pos+12:pos+18])
	copy(stream.Raw[pos+12:pos+18], first)
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 52, G: 86, B: 18, A: 255}) {
		t.Fatalf("definition entry order changed mapping: %v", got)
	}
}

func TestPDFJPXReorderedChannelsUseBudgetCacheAndStrictDiagnostics(t *testing.T) {
	data := reorderedJPX(t, matteJPX(t, "DeviceRGB", 8, []uint16{18, 52, 86}, jpeg2000.FormatJP2), 3, 0, 3, 1, 2)
	object := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data)
	input := budgetImagePDF("/Im0 Do", object)
	// 四个顶层盒、三个子盒与三条关联，加上 256 个分量/输出处理步骤。
	requireBudgetError(t, input, Limits{MaxOperations: 267}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", object), &output, "", Options{Strict: true, Limits: Limits{MaxOperations: 270, MaxTotalImagePixels: 64}}); err != nil {
		t.Fatal(err)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got != (color.NRGBA{R: 52, G: 86, B: 18, A: 255}) {
		t.Fatalf("cached reordered pixel=%v", got)
	}
	// 将两个颜色角色指向同一个关联，必须报告而不能按平面次序继续。
	pos := bytes.Index(data, []byte("cdef")) + 4
	binary.BigEndian.PutUint16(data[pos+12:], 3)
	object = fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data)
	var diagnostics []Diagnostic
	output.Reset()
	err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do", object), &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	if !errors.Is(err, ErrUnsupported) || output.Len() != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "image.decode" {
		t.Fatalf("invalid association err=%v diagnostics=%v output=%d", err, diagnostics, output.Len())
	}
}
