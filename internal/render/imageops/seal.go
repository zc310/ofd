package imageops

import (
	"image"
	"image/color"
	"image/draw"
)

const sealBackgroundCutoff = 250

// SealTransparentBackground 将全不透明印章图中的近白纸张背景置为透明。
// 这是既有印章兼容策略：三个通道均不低于 250 时视为背景。
// 已有透明像素或无法查询 Opaque 的图像直接复用，避免删除其白色图案。
func SealTransparentBackground(img image.Image) image.Image {
	opaque, ok := img.(interface{ Opaque() bool })
	if !ok || !opaque.Opaque() {
		return img
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if r>>8 >= sealBackgroundCutoff && g>>8 >= sealBackgroundCutoff && bl>>8 >= sealBackgroundCutoff {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8)})
		}
	}
	return dst
}

// CropNRGBA 按源图区域生成以零为原点的独立 NRGBA 图片，保留非预乘颜色。
// region 应为源图内的有效非空区域；印章布局层负责区域计算及整图复用判断。
func CropNRGBA(img image.Image, region image.Rectangle) image.Image {
	cropped := image.NewNRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(cropped, cropped.Bounds(), img, region.Min, draw.Src)
	return cropped
}
