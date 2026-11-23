package image

import (
	"image"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func TestOffscreenPlacementPreservesTopLeftCoordinatesAndContainerCTMOrder(t *testing.T) {
	bounds := geom.Rect{X0: 7, Y0: 11, X1: 27, Y1: 41}
	box := CanvasBox(bounds)
	if box != (spec.StBox{X: 7, Y: 11, Width: 20, Height: 30}) {
		t.Fatalf("画布边界不应翻转：%+v", box)
	}
	if got := BoundaryMatrix(box, 200, 300, nil); got != (geom.Matrix{{0.1, 0, 7}, {0, -0.1, 41}}) {
		t.Fatalf("离屏像素未放回原画布边界：%v", got)
	}
	parent := spec.CTM{2, 1, 3, 4, 5, 6}
	// 容器 CTM 在 Boundary 缩放之后作用，仅适配源图片的局部 Y 轴。
	want := geom.Matrix{{0.2, -0.3, 102}, {0.1, -0.4, 137}}
	if got := BoundaryMatrix(box, 200, 300, &parent); got != want {
		t.Fatalf("容器 CTM 的乘法或翻转顺序改变：%v，期望 %v", got, want)
	}
}

func TestOffscreenMatrixKeepsFractionalBoundsAndPixelScale(t *testing.T) {
	bounds := geom.Rect{X0: 7, Y0: 11, X1: 9.01, Y1: 14.01}
	img := image.NewRGBA(image.Rect(3, 5, 24, 36))
	got := OffscreenMatrix(bounds, img, geom.DPMM(10))
	want := geom.Matrix{{0.1, 0, 7}, {0, -0.1, 14.01}}
	for i := range got {
		for j := range got[i] {
			if math.Abs(got[i][j]-want[i][j]) > 1e-12 {
				t.Fatalf("离屏放置矩阵 %v，期望 %v", got, want)
			}
		}
	}
}

// 离屏高度向上取整多出的像素位于顶部；回贴时不能把它当成页面内容，
// 也不能把整张位图压缩回声明边界，否则渐变、文字和底纹会发生亚像素位移。
func TestOffscreenMatrixRestoresFractionalPagePointsAcrossDPI(t *testing.T) {
	bounds := geom.Rect{X0: 2.17, Y0: 7.31, X1: 9.43, Y1: 13.79}
	for _, dpi := range []float64{72, 96, 150, 200, 300} {
		resolution := geom.DPI(dpi)
		dpmm := resolution.DPMM()
		img := image.NewRGBA(image.Rect(0, 0, geom.RasterPixels(bounds.W(), resolution), geom.RasterPixels(bounds.H(), resolution)))
		matrix := OffscreenMatrix(bounds, img, resolution)
		for _, page := range []geom.Point{
			{X: bounds.X0, Y: bounds.Y0},
			{X: bounds.X1, Y: bounds.Y1},
			{X: 5.3, Y: 9.7},
		} {
			// 从实际扫描规则独立得到图片局部 Y 向上像素坐标，不把取整多出的顶部算入内容。
			pixel := geom.Point{X: (page.X - bounds.X0) * dpmm, Y: (bounds.Y1 - page.Y) * dpmm}
			got := matrix.Dot(pixel)
			if math.Abs(got.X-page.X) > 1e-12 || math.Abs(got.Y-page.Y) > 1e-12 {
				t.Fatalf("DPI=%g，页面点%v回贴后为%v", dpi, page, got)
			}
		}
	}
}
