package imageops

import (
	"image"
	"image/color"
)

// ApplyResourceMask 将资源掩码的亮度合成到原图透明度，尺寸不同时按比例取样。
// 掩码完全透明的像素视为零覆盖，缺失或空掩码保持原图。
func ApplyResourceMask(img, mask image.Image) image.Image {
	bounds := img.Bounds()
	if bounds.Empty() || mask == nil || mask.Bounds().Empty() {
		return img
	}
	src := decodedImage(img)
	mask = decodedImage(mask)
	out := image.NewNRGBA(bounds)
	maskBounds := mask.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		my := maskBounds.Min.Y + (y-bounds.Min.Y)*maskBounds.Dy()/bounds.Dy()
		if my >= maskBounds.Max.Y {
			my = maskBounds.Max.Y - 1
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			mx := maskBounds.Min.X + (x-bounds.Min.X)*maskBounds.Dx()/bounds.Dx()
			if mx >= maskBounds.Max.X {
				mx = maskBounds.Max.X - 1
			}
			c := nrgbaAt(src, x, y)
			mc := nrgbaAt(mask, mx, my)
			luma := (299*uint32(mc.R) + 587*uint32(mc.G) + 114*uint32(mc.B)) / 1000
			if mc.A == 0 {
				luma = 0
			}
			c.A = uint8(uint32(c.A) * luma / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	switch source := img.(type) {
	case *image.NRGBA:
		return source.NRGBAAt(x, y)
	case *image.RGBA:
		// 标准库 NRGBAModel 的转换结果固定为 color.NRGBA。
		c, _ := color.NRGBAModel.Convert(source.RGBAAt(x, y)).(color.NRGBA)
		return c
	default:
		c, _ := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
		return c
	}
}
