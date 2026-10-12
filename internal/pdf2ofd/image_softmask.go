package pdf2ofd

import (
	"fmt"
	"image"
	"image/color"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// applyPDFImageSoftMask16 保留底图的非预乘颜色及完整的 16 位透明度。
// 输出保留两者各轴最大分辨率，底图与掩码分别按最近邻对齐。
func applyPDFImageSoftMask16(ctx *model.Context, base image.Image, mask *types.StreamDict, maskWidth, maskHeight, bpc int, control *conversionControl) ([]byte, bool) {
	var samples16 []uint16
	var samples8 []byte
	var err error
	if bpc == 16 && !hasDCTFilter(mask) && !hasJPXFilter(mask) && !hasJBIG2Filter(mask) {
		samples16, err = decodePDFImageSamples16(ctx, mask, maskWidth, maskHeight, 1, control)
	} else {
		if control.consumeOperations(int64(maskWidth)*int64(maskHeight)) != nil {
			return nil, false
		}
		samples8, err = pdfImageMaskSamples(ctx, mask, maskWidth, maskHeight, bpc, control)
	}
	if err != nil || control.check() != nil {
		return nil, false
	}
	bounds := base.Bounds()
	width, height, err := pdfSoftMaskCompositionSize(bounds, maskWidth, maskHeight, control)
	if err != nil {
		return nil, false
	}
	if control.consumeOperations(int64(width)*int64(height)) != nil {
		return nil, false
	}
	output := image.NewNRGBA64(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		if control.check() != nil {
			return nil, false
		}
		maskRow := pdfSoftMaskRow(y, maskHeight, height) * maskWidth
		sourceY := bounds.Min.Y + pdfSoftMaskRow(y, bounds.Dy(), height)
		for x := 0; x < width; x++ {
			sourceX := bounds.Min.X + pdfSoftMaskColumn(x, bounds.Dx(), width)
			value := pdfImageNRGBA64At(base, sourceX, sourceY)
			index := maskRow + pdfSoftMaskColumn(x, maskWidth, width)
			if samples16 != nil {
				value.A = samples16[index]
			} else {
				value.A = uint16(samples8[index]) * 257
			}
			output.SetNRGBA64(x, y, value)
		}
	}
	data, _, err := encodePNGImage(output)
	return data, err == nil && control.check() == nil
}

// 软掩码可以比底图更清晰；例如单像素纯色配扫描轮廓。合成取两者各轴
// 最大值，避免下采样把细线、局部透明和相邻 alpha 丢掉。基础 PNG 重新
// 解码已预留源尺寸，扩展时只追加合成栅格增加的像素；交叉长宽也须预检。
func pdfSoftMaskCompositionSize(bounds image.Rectangle, maskWidth, maskHeight int, control *conversionControl) (int, int, error) {
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 || maskWidth <= 0 || maskHeight <= 0 {
		return 0, 0, fmt.Errorf("软掩码合成尺寸无效")
	}
	width, height := max(sourceWidth, maskWidth), max(sourceHeight, maskHeight)
	if control != nil {
		pixels, err := control.imagePixels(int64(width), int64(height))
		if err != nil {
			return 0, 0, err
		}
		if err := control.addImagePixels(pixels - int64(sourceWidth)*int64(sourceHeight)); err != nil {
			return 0, 0, err
		}
	}
	return width, height, nil
}

// pdfImageNRGBA64At 避免逐像素通过 image.At 装箱。已是非预乘的底图直接取值，
// 防止低 alpha 下往返预乘损失颜色；其余颜色按 PNG 的非预乘通道语义转换。
func pdfImageNRGBA64At(img image.Image, x, y int) color.NRGBA64 {
	switch source := img.(type) {
	case *image.NRGBA64:
		return source.NRGBA64At(x, y)
	case *image.RGBA64:
		v := source.RGBA64At(x, y)
		if v.A == 65535 {
			return color.NRGBA64{R: v.R, G: v.G, B: v.B, A: v.A}
		}
		return color.NRGBA64Model.Convert(v).(color.NRGBA64)
	case *image.Gray16:
		v := source.Gray16At(x, y).Y
		return color.NRGBA64{R: v, G: v, B: v, A: 65535}
	case *image.Gray:
		v := uint16(source.GrayAt(x, y).Y) * 257
		return color.NRGBA64{R: v, G: v, B: v, A: 65535}
	case *image.NRGBA:
		v := source.NRGBAAt(x, y)
		return color.NRGBA64{R: uint16(v.R) * 257, G: uint16(v.G) * 257, B: uint16(v.B) * 257, A: uint16(v.A) * 257}
	case *image.RGBA:
		v := source.RGBAAt(x, y)
		if v.A == 255 {
			return color.NRGBA64{R: uint16(v.R) * 257, G: uint16(v.G) * 257, B: uint16(v.B) * 257, A: 65535}
		}
		return color.NRGBA64Model.Convert(v).(color.NRGBA64)
	case *image.YCbCr:
		v := source.YCbCrAt(x, y)
		r, g, b := color.YCbCrToRGB(v.Y, v.Cb, v.Cr)
		return color.NRGBA64{R: uint16(r) * 257, G: uint16(g) * 257, B: uint16(b) * 257, A: 65535}
	default:
		return color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
	}
}
