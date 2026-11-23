package render

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageops"
)

// compositeClipRegion 缩小软件裁剪掩码的窗口，不修改最终图片尺寸及放置矩阵。
// GB/T 33190—2016 §8.4、§13 表50：仅减少裁剪外的处理，保留复合对象的映射。
// 裁剪路径是像素空间中 Y 向上的坐标；图像行从上向下，切片时须反转 Y。
func compositeClipRegion(img image.Image, clip *geom.Path) (image.Rectangle, *geom.Path, bool) {
	source, ok := img.(*image.RGBA)
	if !ok || clip == nil || clip.Empty() {
		return image.Rectangle{}, nil, false
	}
	bounds := clip.Bounds()
	if !finiteFloat(bounds.X0) || !finiteFloat(bounds.Y0) || !finiteFloat(bounds.X1) || !finiteFloat(bounds.Y1) {
		return image.Rectangle{}, nil, false
	}
	size := source.Bounds().Size()
	// 对齐源像素网格，并保留两像素边，完整覆盖掩码抗锯齿。
	// 先夹到合法像素范围再转整数，避免异常路径坐标溢出。
	x0 := int(max(0, min(float64(size.X), math.Floor(bounds.X0)-2)))
	x1 := int(max(0, min(float64(size.X), math.Ceil(bounds.X1)+2)))
	y0 := int(max(0, min(float64(size.Y), math.Floor(bounds.Y0)-2)))
	y1 := int(max(0, min(float64(size.Y), math.Ceil(bounds.Y1)+2)))
	area := int64(x1-x0) * int64(y1-y0)
	if x1 <= x0 || y1 <= y0 || area > int64(size.X)*int64(size.Y)*3/4 {
		return image.Rectangle{}, nil, false
	}
	crop := image.Rect(x0, size.Y-y1, x1, size.Y-y0).Add(source.Rect.Min)
	return crop, clip.Copy().Translate(-float64(x0), -float64(y0)), true
}

// compositeImageWithClipAlpha 保留完整输出画布，避免改变 PDF 阅读器的图像插值相位。
// 掩码只覆盖必要窗口；窗口外的像素透明，不再扫描源图或执行反预乘运算。
func compositeImageWithClipAlpha(img image.Image, clip *geom.Path, alpha uint8, budget *renderBudget) (image.Image, bool) {
	if region, local, ok := compositeClipRegion(img, clip); ok {
		if !allowImageAllocation(img, 1, budget) ||
			!budget.AllowOffscreenPixels(float64(region.Dx()), float64(region.Dy()), 25.4) {
			return nil, false
		}
		mask := imageClipMask(local, region.Dx(), region.Dy())
		if mask == nil {
			return image.NewNRGBA(img.Bounds()), true
		}
		source, _ := img.(*image.RGBA)
		return imageops.ApplyMaskAlphaRegion(source, mask, region, alpha), true
	}
	copies := 0
	if clip != nil {
		copies = 2
	} else if alpha != 255 {
		copies = 1
	}
	if copies > 0 && !allowImageAllocation(img, copies, budget) {
		return nil, false
	}
	return imageWithLocalClipAlpha(img, clip, alpha), true
}
