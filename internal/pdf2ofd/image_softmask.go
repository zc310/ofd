package pdf2ofd

import (
	"image"
	"image/color"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// applyPDFImageSoftMask16 保留底图的非预乘颜色及完整的 16 位透明度。
// 输出尺寸沿用基础图，掩码按最近邻对齐，与原有 8 位软掩码路径一致。
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
		samples8, err = pdfImageMaskSamples(ctx, mask, maskWidth, maskHeight, bpc)
	}
	if err != nil || control.check() != nil {
		return nil, false
	}
	bounds := base.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if control.consumeOperations(int64(width)*int64(height)) != nil {
		return nil, false
	}
	output := image.NewNRGBA64(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		if control.check() != nil {
			return nil, false
		}
		maskRow := pdfSoftMaskRow(y, maskHeight, height) * maskWidth
		for x := 0; x < width; x++ {
			value := pdfImageNRGBA64At(base, bounds.Min.X+x, bounds.Min.Y+y)
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
