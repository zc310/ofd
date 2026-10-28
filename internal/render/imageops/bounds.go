package imageops

import (
	"image"
	"image/draw"
)

// ContentBounds 返回 Alpha 大于 8 的内容像素包围盒，保留图像原点，右下边界不包含。
// 空白图片返回全零，阈值与渲染器测量复合单元的既有行为一致。
func ContentBounds(img image.Image) (float64, float64, float64, float64) {
	b := img.Bounds()
	switch src := img.(type) {
	case *image.NRGBA:
		return scanPixAlphaBounds(src.Pix, src.Stride, b)
	case *image.RGBA:
		return scanPixAlphaBounds(src.Pix, src.Stride, b)
	default:
		return scanImageAlphaBounds(img, b)
	}
}

func scanPixAlphaBounds(pix []byte, stride int, b image.Rectangle) (float64, float64, float64, float64) {
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	found := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := pix[(y-b.Min.Y)*stride:]
		for x := b.Min.X; x < b.Max.X; x++ {
			if row[(x-b.Min.X)*4+3] <= 8 {
				continue
			}
			found = true
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if !found {
		return 0, 0, 0, 0
	}
	return float64(minX), float64(minY), float64(maxX + 1), float64(maxY + 1)
}

func scanImageAlphaBounds(img image.Image, b image.Rectangle) (float64, float64, float64, float64) {
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	found := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha <= 8*257 {
				continue
			}
			found = true
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if !found {
		return 0, 0, 0, 0
	}
	return float64(minX), float64(minY), float64(maxX + 1), float64(maxY + 1)
}

// Crop 按原图像素区域生成以零为原点的独立 RGBA 图片。
func Crop(img image.Image, x0, y0, x1, y1 int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(out, out.Bounds(), img, image.Point{X: x0, Y: y0}, draw.Src)
	return out
}
