package pdf2ofd

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image/color"
	"math"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func jpxWithColourEnum(t *testing.T, data []byte, enum uint32) []byte {
	t.Helper()
	data = bytes.Clone(data)
	pos := bytes.Index(data, []byte("colr"))
	if pos < 0 {
		t.Fatal("missing colr")
	}
	binary.BigEndian.PutUint32(data[pos+7:], enum)
	return data
}

func TestPDFJPXColourConversionFollowsReorderedLogicalComponents(t *testing.T) {
	for _, tc := range []struct {
		name string
		enum uint32
		want [3]float64
	}{
		{"sYCC", 18, [3]float64{214.964, 27.00932, 174.424}},
		{"YCbCr2", 3, [3]float64{214.964, 27.00932, 174.424}},
		{"YCbCr3", 4, [3]float64{214.964, 27.00932, 174.424}},
		{"eYCC", 24, [3]float64{214.9616344, 27.0415004, 174.407324}},
		{"PhotoYCC", 9, [3]float64{173.3536, 51.2362, 131.0506}},
		{"CMY", 11, [3]float64{155, 85, 45}},
	} {
		for _, bits := range []int{8, 12, 16} {
			t.Run(fmt.Sprintf("%s_%d", tc.name, bits), func(t *testing.T) {
				data := matteJPX(t, "DeviceRGB", bits, []uint16{210, 100, 170}, jpeg2000.FormatJP2)
				data = jpxWithColourEnum(t, data, tc.enum)
				data = reorderedJPX(t, data, 3, 0, 3, 1, 2)
				stream := jpxMatteStream("DeviceRGB", data)
				stream.Dict["Decode"] = types.Array{types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(1), types.Integer(0)}
				img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
				got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64)
				// 8→12/16 位编码缩放以及色度中心差异会带来不足一个 8 位等级误差。
				for c, v := range []uint16{got.R, got.G, got.B} {
					if math.Abs(float64(v)/257-tc.want[c]) > 1 {
						t.Fatalf("channel %d=%v want %v pixel=%#v", c, float64(v)/257, tc.want[c], got)
					}
				}
				if got.A != 65535 {
					t.Fatalf("alpha=%d", got.A)
				}
			})
		}
	}
}

func TestPDFJPXSYCCMatchesIndependent601Reference(t *testing.T) {
	// Go 的标准 YCbCrToRGB 使用独立定点 BT.601 实现；非中性、边界和
	// 超色域输入保护系数及截断顺序，不与被测实现共用矩阵。
	for _, sample := range [][3]byte{{100, 170, 210}, {180, 90, 50}, {0, 128, 128}, {255, 128, 128}, {0, 0, 255}, {255, 255, 0}} {
		data := jpxWithColourEnum(t, matteJPX(t, "DeviceRGB", 8, []uint16{uint16(sample[0]), uint16(sample[1]), uint16(sample[2])}, jpeg2000.FormatJP2), 18)
		img := jpxDecodedImage(t, &pdfInterpreter{}, jpxMatteStream("DeviceRGB", data))
		got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA)
		r, g, b := color.YCbCrToRGB(sample[0], sample[1], sample[2])
		for c, v := range []byte{got.R, got.G, got.B} {
			want := []byte{r, g, b}[c]
			if difference := int(v) - int(want); difference < -1 || difference > 1 {
				t.Fatalf("sYCC %v=%v want %v,%v,%v", sample, got, r, g, b)
			}
		}
	}
}

func TestPDFJPXConvertedColoursKeepBudgetAndCachedResult(t *testing.T) {
	data := reorderedJPX(t, jpxWithColourEnum(t, matteJPX(t, "DeviceRGB", 8, []uint16{210, 100, 170}, jpeg2000.FormatJP2), 18), 3, 0, 3, 1, 2)
	object := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data)
	requireBudgetError(t, budgetImagePDF("/Im0 Do", object), Limits{MaxOperations: 459}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", object), &output, "", Options{Strict: true, Limits: Limits{MaxOperations: 462, MaxTotalImagePixels: 64}}); err != nil {
		t.Fatal(err)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got != (color.NRGBA{215, 27, 174, 255}) {
		t.Fatalf("converted cached colour=%v", got)
	}
}

func TestPDFJPXColourTransformsRemainUnsupportedForMatteAndOpacity(t *testing.T) {
	data := jpxWithColourEnum(t, matteJPX(t, "DeviceRGB", 8, []uint16{100, 170, 210}, jpeg2000.FormatJP2), 18)
	p := pdfInterpreter{}
	if _, err := p.matteJPXHeader(jpxMatteStream("DeviceRGB", data)); err == nil {
		t.Fatal("accepted colour transform in Matte")
	}
	opacity := jpxAlphaStream(t, 8, 1, color.NRGBA64{R: 25700, G: 43690, B: 53970, A: 32896})
	opacity.Raw = jpxWithColourEnum(t, opacity.Raw, 18)
	if _, err := p.jpxHeader(opacity, true); err == nil {
		t.Fatal("accepted unimplemented transformed opacity")
	}
}

func TestPDFJPXIndependentOpenJPEGReorderedSYCC(t *testing.T) {
	// OpenJPEG 2.5.0 编码的原平面 (Cr=210,Y=100,Cb=170)，colr=18，
	// cdef 关联 (3,1,2)。MuPDF 源渲染中心 RGB 为 (214,26,174)。
	data, err := base64.StdEncoding.DecodeString("AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAABJanAyaAAAABZpaGRyAAAACAAAAAgAAwcHAAAAAAAPY29scgEAAAAAABIAAAAcY2RlZgADAAAAAAADAAEAAAABAAIAAAACAAAAwmpwMmP/T/9RAC8AAAAAAAgAAAAIAAAAAAAAAAAAAAAIAAAACAAAAAAAAAAAAAMHAQEHAQEHAQH/UgAMAAAAAQEABAQAAf9cAARAQP9kACUAAUNyZWF0ZWQgYnkgT3BlbkpQRUcgdmVyc2lvbiAyLjUuMP+QAAoAAAAAAEoAAf+Tw+caFABco2r9SAAAADCQj8+0TBQAXKNq/UgAAAAwkJd16wAMJCPPtEwUAFyv+c2AAAGEhL6+ZgAAYSEf/9k=")
	if err != nil {
		t.Fatal(err)
	}
	img := jpxDecodedImage(t, &pdfInterpreter{}, jpxMatteStream("DeviceRGB", data))
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{215, 27, 174, 255}) {
		t.Fatalf("independent sYCC=%v", got)
	}
}
