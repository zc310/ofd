// Package seal 计算印章裁剪框与源图像素区域，不加载印章或执行绘制。
package seal

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/models"
)

// ClipBox 将 StampAnnot.Clip 夹入 Boundary 并返回页面上的目标框。
// Boundary 与 Clip 分别对应 GB/T 33190—2016 §18.2.3 表69 的定位与裁剪属性。
// Clip 相对印章左上角；缺省、零尺寸或完全位于盒外时返回 false，沿用整枚印章。
func ClipBox(boundary, clip models.StBox) (models.StBox, bool) {
	if boundary.Width <= 0 || boundary.Height <= 0 || clip.Width <= 0 || clip.Height <= 0 {
		return models.StBox{}, false
	}
	x0, y0 := math.Max(clip.X, 0), math.Max(clip.Y, 0)
	x1, y1 := math.Min(clip.X+clip.Width, boundary.Width), math.Min(clip.Y+clip.Height, boundary.Height)
	if x1 <= x0 || y1 <= y0 {
		return models.StBox{}, false
	}
	return models.StBox{X: boundary.X + x0, Y: boundary.Y + y0, Width: x1 - x0, Height: y1 - y0}, true
}

// CropRect 按目标框相对 Boundary 的比例计算源图裁剪区域，保留原图非零原点。
// 坐标按最近像素取整并夹回原图。无有效区域或覆盖整图时返回 false，复用原图。
func CropRect(bounds image.Rectangle, boundary, dst models.StBox) (image.Rectangle, bool) {
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || boundary.Width <= 0 || boundary.Height <= 0 {
		return image.Rectangle{}, false
	}
	sx := func(v float64) int {
		return clampInt(int(math.Round(float64(bounds.Min.X)+v)), bounds.Min.X, bounds.Max.X)
	}
	sy := func(v float64) int {
		return clampInt(int(math.Round(float64(bounds.Min.Y)+v)), bounds.Min.Y, bounds.Max.Y)
	}
	x0 := sx((dst.X - boundary.X) / boundary.Width * float64(w))
	y0 := sy((dst.Y - boundary.Y) / boundary.Height * float64(h))
	x1 := sx((dst.X + dst.Width - boundary.X) / boundary.Width * float64(w))
	y1 := sy((dst.Y + dst.Height - boundary.Y) / boundary.Height * float64(h))
	if x0 >= x1 || y0 >= y1 {
		return image.Rectangle{}, false
	}
	region := image.Rect(x0, y0, x1, y1)
	if region == bounds {
		return region, false
	}
	return region, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
