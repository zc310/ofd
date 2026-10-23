package pdf2ofd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/color"
	"math"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
)

func TestPDFJPXEYCCMatchesOpenJPEGGoldenSamples(t *testing.T) {
	// 固定值来自 OpenJPEG 2.5.0 color_esycc_to_rgb 的单精度参考计算。
	// 覆盖中性、非中性、负值/超色域截断，避免只测灰色而掩盖系数错误。
	for _, tc := range []struct {
		bits    int
		samples [3]uint16
		want    [3]uint16
	}{
		{8, [3]uint16{100, 170, 210}, [3]uint16{215, 27, 174}},
		{8, [3]uint16{180, 90, 50}, [3]uint16{71, 249, 113}},
		{8, [3]uint16{0, 128, 128}, [3]uint16{0, 0, 0}},
		{8, [3]uint16{255, 128, 128}, [3]uint16{255, 255, 255}},
		{8, [3]uint16{0, 0, 255}, [3]uint16{178, 0, 0}},
		{8, [3]uint16{255, 255, 0}, [3]uint16{76, 255, 255}},
		{12, [3]uint16{1600, 2700, 3300}, [3]uint16{3355, 482, 2755}},
		{16, [3]uint16{25700, 43690, 53970}, [3]uint16{55425, 6809, 45050}},
		{16, [3]uint16{0, 32768, 32768}, [3]uint16{0, 0, 0}},
		{16, [3]uint16{65535, 32768, 32768}, [3]uint16{65535, 65535, 65523}},
		{16, [3]uint16{0, 0, 65535}, [3]uint16{45940, 0, 0}},
		{16, [3]uint16{65535, 65535, 0}, [3]uint16{19593, 65535, 65535}},
	} {
		t.Run(fmt.Sprintf("%d_%v", tc.bits, tc.samples), func(t *testing.T) {
			maximum := float64(uint32(1)<<uint(tc.bits) - 1)
			values := [4]float64{float64(tc.samples[0]) / maximum, float64(tc.samples[1]) / maximum, float64(tc.samples[2]) / maximum, 1}
			got := jpxColourToRGB(values, jpeg2000.ColorSpaceEYCC, tc.bits)
			for c, want := range tc.want {
				if math.Abs(got[c]*maximum-float64(want)) > .51 {
					t.Fatalf("channel %d=%v want %d", c, got[c]*maximum, want)
				}
			}
		})
	}
}

func TestPDFJPXEYCCReordersBeforeMatrixAndPreservesHighDepth(t *testing.T) {
	// 16 位输出必须保留低位：旧矩阵可产生相似灰色，但这些非中性样本
	// 既检测矩阵，也检测归一化中心及 8 位中间结果造成的精度损失。
	for _, bits := range []int{8, 12, 16} {
		data := reorderedJPX(t, jpxWithColourEnum(t, matteJPX(t, "DeviceRGB", bits, []uint16{210, 100, 170}, jpeg2000.FormatJP2), 24), 3, 0, 3, 1, 2)
		img := jpxDecodedImage(t, &pdfInterpreter{}, jpxMatteStream("DeviceRGB", data))
		got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64)
		if bits == 8 && got != (color.NRGBA64{R: 215 * 257, G: 27 * 257, B: 174 * 257, A: 65535}) {
			t.Fatalf("8-bit eYCC=%#v", got)
		}
		if bits == 16 && got != (color.NRGBA64{R: 55425, G: 6809, B: 45050, A: 65535}) {
			t.Fatalf("16-bit eYCC precision=%#v", got)
		}
	}
}

func TestPDFJPXEYCCStrictConversionUsesBudgetAndCache(t *testing.T) {
	data := reorderedJPX(t, jpxWithColourEnum(t, matteJPX(t, "DeviceRGB", 8, []uint16{210, 100, 170}, jpeg2000.FormatJP2), 24), 3, 0, 3, 1, 2)
	object := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data)
	requireBudgetError(t, budgetImagePDF("/Im0 Do", object), Limits{MaxOperations: 459}, "operations")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), budgetImagePDF("/Im0 Do /Im0 Do", object), &output, "", Options{Strict: true, Limits: Limits{MaxOperations: 462, MaxTotalImagePixels: 64}}); err != nil {
		t.Fatal(err)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got != (color.NRGBA{R: 215, G: 27, B: 174, A: 255}) {
		t.Fatalf("cached eYCC=%v", got)
	}
}

func TestPDFJPXEYCCIndependentOpenJPEGReorderedCodestream(t *testing.T) {
	// 独立 OpenJPEG 编码、原平面 Cr/Y/Cb=(210,100,170)，cdef=(3,1,2)，
	// colr=24；原样本经 OpenJPEG eYCC 参考矩阵得到 RGB=(215,27,174)。
	data, err := base64.StdEncoding.DecodeString("AAAADGpQICANCocKAAAAFGZ0eXBqcDIgAAAAAGpwMiAAAABJanAyaAAAABZpaGRyAAAACAAAAAgAAwcHAAAAAAAPY29scgEAAAAAABgAAAAcY2RlZgADAAAAAAADAAEAAAABAAIAAAACAAAAwmpwMmP/T/9RAC8AAAAAAAgAAAAIAAAAAAAAAAAAAAAIAAAACAAAAAAAAAAAAAMHAQEHAQEHAQH/UgAMAAAAAQEABAQAAf9cAARAQP9kACUAAUNyZWF0ZWQgYnkgT3BlbkpQRUcgdmVyc2lvbiAyLjUuMP+QAAoAAAAAAEoAAf+Tw+caFABco2r9SAAAADCQj8+0TBQAXKNq/UgAAAAwkJd16wAMJCPPtEwUAFyv+c2AAAGEhL6+ZgAAYSEf/9k=")
	if err != nil {
		t.Fatal(err)
	}
	img := jpxDecodedImage(t, &pdfInterpreter{}, jpxMatteStream("DeviceRGB", data))
	if got := color.NRGBAModel.Convert(img.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 215, G: 27, B: 174, A: 255}) {
		t.Fatalf("independent eYCC pixel=%v", got)
	}
}
