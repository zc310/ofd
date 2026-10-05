// 本文件覆盖 PDF→OFD 转换里两处 OFD 标准无法直接表达、只能近似的图形效果：
// ExtGState 的 Luminosity 软掩码（烘进图像 alpha）与 Multiply 混合模式
// （近似为半透明叠加）。
package pdf2ofd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// maskedImagePDF 构造一份最小 PDF：ExtGState 的 /SMask 指向亮度掩码，掩码的 /G
// 是一个只调用一次 Do 绘制灰度图像的 Form XObject —— Illustrator CS 导出高光与
// 柔边图层就是这个三层嵌套形态。mask 为掩码图像的原始样本。
func maskedImagePDF(mask string, maskW, maskH int, gsObject string) []byte {
	content := []byte("q /GS0 gs 20 0 0 20 0 0 cm /Im0 Do Q")
	maskForm := []byte("q 2 0 0 2 0 0 cm /MaskIm Do Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 20 20] /Resources << /ExtGState << /GS0 " + gsObject + " >> /XObject << /Im0 4 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 12 >>\nstream\n\xff\x00\x00\xff\x00\xff\x00\xff\x00\x00\xff\xff\xff\xff\nendstream",
		"<< /Type /XObject /Subtype /Image /Width " + itoa(maskW) + " /Height " + itoa(maskH) + " /ColorSpace /DeviceGray /BitsPerComponent 8 /Length " + itoa(len(mask)) + " >>\nstream\n" + mask + "\nendstream",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /Mask /S /Luminosity /G 8 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 2 2] /Resources << /XObject << /MaskIm 5 0 R >> >> /Length " + itoa(len(maskForm)) + " >>\nstream\n" + string(maskForm) + "\nendstream",
		"<< /Type /ExtGState /ca 1 /CA 1 /SMask 7 0 R >>",
	}
	return assemblePDF(objects)
}

func TestExtGStateLuminosityMaskBakedIntoImageAlpha(t *testing.T) {
	// 掩码左列全黑（完全透明）、右列全白（完全不透明）。
	pdf := maskedImagePDF("\x00\xff", 2, 1, "9 0 R")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
		t.Fatal(err)
	}
	img := decodeFirstPageImage(t, output.Bytes())
	// 左列应被掩码压成透明，右列保持不透明。
	if alpha := img.NRGBAAt(0, 0).A; alpha != 0 {
		t.Errorf("掩码黑侧 alpha = %d，期望 0（ExtGState 软掩码未生效）", alpha)
	}
	if alpha := img.NRGBAAt(1, 0).A; alpha != 0xff {
		t.Errorf("掩码白侧 alpha = %d，期望 255", alpha)
	}
}

func TestExtGStateSoftMaskNoneKeepsImageOpaque(t *testing.T) {
	// /SMask /None 不应解析出掩码，图像保持完全不透明。
	pdf := maskedImagePDF("\x00\x00", 2, 1, "9 0 R")
	pdf = bytes.Replace(pdf, []byte("/SMask 7 0 R"), []byte("/SMask /None"), 1)
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
		t.Fatal(err)
	}
	img := decodeFirstPageImage(t, output.Bytes())
	if alpha := img.NRGBAAt(0, 0).A; alpha != 0xff {
		t.Errorf("/SMask /None 时 alpha = %d，期望 255", alpha)
	}
}

func TestApproximateMultiplyGradientAppliesPerStop(t *testing.T) {
	interpreter := &pdfInterpreter{state: pdfGraphicsState{blendMode: "Multiply", fillAlpha: 1}}
	color := &creator.Color{Axial: &creator.AxialShading{Segments: []creator.ColorStop{
		{Position: 0, PositionSet: true, Color: creator.Color{R: 200, G: 150, B: 150}},
		{Position: 1, PositionSet: true, Color: creator.Color{R: 255, G: 255, B: 255}},
	}}}
	if !interpreter.approximateMultiplyGradient(color, 1) {
		t.Fatal("轴向渐变的 Multiply 近似未生效")
	}
	segments := color.Axial.Segments
	alpha := segments[0].Color.Alpha
	if alpha == nil || *alpha == 0xff {
		t.Errorf("中间色色标 Alpha = %v，期望按最暗分量推出的非完全不透明值", alpha)
	}
	// a = op·(255−minC)/255 = (255−150)/255 ≈ 0.412
	if alpha != nil && (*alpha < 100 || *alpha > 110) {
		t.Errorf("色标 Alpha = %d，期望约 105（(255−150)/255）", *alpha)
	}
	// 修正色把最暗分量压到 0，深色内容仍能透出。
	if segments[0].Color.G != 0 || segments[0].Color.B != 0 {
		t.Errorf("修正色 = (%d,%d,%d)，期望最暗分量被压到 0", segments[0].Color.R, segments[0].Color.G, segments[0].Color.B)
	}
	if segments[0].Color.R >= 200 {
		t.Errorf("修正色 R = %d，期望被反推为更亮的高亮色", segments[0].Color.R)
	}
	// 纯白色色标下 Multiply 与 Normal 等价，不应改写。
	if segments[1].Color.Alpha != nil {
		t.Errorf("白色色标不应被改写，Alpha = %d", *segments[1].Color.Alpha)
	}
}

func TestApproximateMultiplyMeshGradientApplies(t *testing.T) {
	interpreter := &pdfInterpreter{state: pdfGraphicsState{blendMode: "Multiply", fillAlpha: 1}}
	color := &creator.Color{Gouraud: &creator.GouraudShading{
		Points: []creator.GouraudPoint{
			{X: 0, Y: 0, Color: creator.Color{R: 200, G: 150, B: 150}},
			{X: 5, Y: 0, Color: creator.Color{R: 255, G: 255, B: 255}},
		},
	}}
	if !interpreter.approximateMultiplyGradient(color, 1) {
		t.Fatal("网格渐变的 Multiply 近似未生效")
	}
	if color.Gouraud.Points[0].Color.Alpha == nil {
		t.Error("网格渐变的中间色控制点应带上 Alpha")
	}
	if color.Gouraud.Points[1].Color.Alpha != nil {
		t.Error("网格渐变的白色控制点不应被改写")
	}
}

// decodeFirstPageImage 解析 OFD 并解码首页第一张图片。
func decodeFirstPageImage(t *testing.T, ofdBytes []byte) *image.NRGBA {
	t.Helper()
	document, err := parser.NewOFD(ofdBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer document.Close()
	page := document.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	images := layerImages(page.Content().Layer[0])
	if len(images) == 0 {
		t.Fatal("转换结果里没有图片对象")
	}
	media := document.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil {
		t.Fatal("图片对象缺少资源")
	}
	data, err := document.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		bounds := decoded.Bounds()
		nrgba = image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		for y := 0; y < bounds.Dy(); y++ {
			for x := 0; x < bounds.Dx(); x++ {
				nrgba.SetNRGBA(x, y, color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA))
			}
		}
	}
	return nrgba
}
