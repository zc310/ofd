package gio

import (
	"image"
	"image/draw"
)

// image8BitForGio 直接读取 RGBA/NRGBA 行缓冲，避免 At 和 color.Model 逐像素
// 装箱。只在发现半透明像素时分配目标图，不修改输入，不跨调用缓存转换结果。
func image8BitForGio(src image.Image, pixels []byte, stride int, straight bool) image.Image {
	bounds := src.Bounds()
	var dst *image.RGBA
	var table *[256][256]uint8
	for y := range bounds.Dy() {
		row := pixels[y*stride : y*stride+bounds.Dx()*4]
		for x := 0; x < len(row); x += 4 {
			alpha := row[x+3]
			if alpha == 0 || alpha == 255 {
				continue
			}
			if dst == nil {
				dst = image.NewRGBA(image.Rectangle{Max: bounds.Size()})
				draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
				table = linearPremulTable()
			}
			r, g, b := row[x], row[x+1], row[x+2]
			if !straight {
				// 与 color.NRGBAModel 对 8 位预乘通道的 16 位除法、截断
				// 完全一致，不用近似的 channel*255/alpha 改变舍入。
				r = uint8((uint32(r) * 0xffff / uint32(alpha)) >> 8)
				g = uint8((uint32(g) * 0xffff / uint32(alpha)) >> 8)
				b = uint8((uint32(b) * 0xffff / uint32(alpha)) >> 8)
			}
			i := y*dst.Stride + x
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2] = table[alpha][r], table[alpha][g], table[alpha][b]
		}
	}
	if dst != nil {
		return dst
	}
	return imageWithZeroOrigin(src)
}

func imageWithZeroOrigin(src image.Image) image.Image {
	if src.Bounds().Min == (image.Point{}) {
		return src
	}
	dst := image.NewRGBA(image.Rectangle{Max: src.Bounds().Size()})
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}
