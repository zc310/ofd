package pdf

import "image"

// pdfImageTrimRect 仅裁减内存 RGBA/NRGBA 图片大块、完全透明的外围，不触发编码
// 图片解码。返回源图坐标下的矩形；保留一像素透明边，维持 PDF 插值边界。
// 绘制时保留源像素到页面的原变换，符合 GB/T 33190—2016 §8.1.4、§8.1.5（印刷页25）。
func pdfImageTrimRect(img image.Image) image.Rectangle {
	var pix []byte
	var stride int
	bounds := img.Bounds()
	switch source := img.(type) {
	case *image.NRGBA:
		pix, stride = source.Pix, source.Stride
	case *image.RGBA:
		pix, stride = source.Pix, source.Stride
	default:
		return bounds
	}
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return bounds
	}
	// 任意一条对角线的两个角均可见，就已证明包围盒覆盖全图。
	if pix[3] != 0 && pix[(height-1)*stride+(width-1)*4+3] != 0 ||
		pix[(width-1)*4+3] != 0 && pix[(height-1)*stride+3] != 0 {
		return bounds
	}
	first, last := 0, height-1
	for first < height && !pdfImageRowHasAlpha(pix[first*stride:first*stride+width*4]) {
		first++
	}
	if first == height {
		// 全透明图片保持原流程，避免影响透明蒙版数量及特殊编码路径。
		return bounds
	}
	for last > first && !pdfImageRowHasAlpha(pix[last*stride:last*stride+width*4]) {
		last--
	}
	left, right := width, -1
	for y := first; y <= last && (left > 0 || right < width-1); y++ {
		row := pix[y*stride : y*stride+width*4]
		for x := 0; x < left; x++ {
			if row[x*4+3] != 0 {
				left = x
				break
			}
		}
		for x := width - 1; x > right; x-- {
			if row[x*4+3] != 0 {
				right = x
				break
			}
		}
	}
	crop := image.Rect(max(0, left-1), max(0, first-1), min(width, right+2), min(height, last+2)).Add(bounds.Min)
	// 小幅裁边不值得额外改变图像几何；只处理至少省去四分之一像素的情况。
	if int64(crop.Dx())*int64(crop.Dy()) > int64(width)*int64(height)*3/4 {
		return bounds
	}
	return crop
}

func pdfImageRowHasAlpha(row []byte) bool {
	for i := 3; i < len(row); i += 4 {
		if row[i] != 0 {
			return true
		}
	}
	return false
}
