package image

import (
	"image"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func TestOffscreenPlacementPreservesYFlipAndContainerCTMOrder(t *testing.T) {
	bounds := geom.Rect{X0: 7, Y0: 11, X1: 27, Y1: 41}
	box := CanvasBox(bounds, 100)
	if box != (spec.StBox{X: 7, Y: 59, Width: 20, Height: 30}) {
		t.Fatalf("画布边界翻转错误：%+v", box)
	}
	if got := BoundaryMatrix(box, 200, 300, nil, 100); got != (geom.Matrix{{0.1, 0, 7}, {0, 0.1, 11}}) {
		t.Fatalf("离屏像素未放回原画布边界：%v", got)
	}
	parent := spec.CTM{2, 1, 3, 4, 5, 6}
	// 容器 CTM 在 Boundary 缩放之后作用，翻转只在输出到画布时执行一次。
	want := geom.Matrix{{0.2, -0.3, 102}, {-0.1, 0.4, -85}}
	if got := BoundaryMatrix(box, 200, 300, &parent, 100); got != want {
		t.Fatalf("容器 CTM 的乘法或翻转顺序改变：%v，期望 %v", got, want)
	}
}

func TestOffscreenMatrixKeepsFractionalBoundsAndPixelScale(t *testing.T) {
	bounds := geom.Rect{X0: 7, Y0: 11, X1: 9.01, Y1: 14.01}
	img := image.NewRGBA(image.Rect(3, 5, 24, 36))
	got := OffscreenMatrix(bounds, img, geom.DPMM(10), 100)
	want := geom.Matrix{{0.1, 0, 7}, {0, 0.1, 11}}
	for i := range got {
		for j := range got[i] {
			if math.Abs(got[i][j]-want[i][j]) > 1e-12 {
				t.Fatalf("离屏放置矩阵 %v，期望 %v", got, want)
			}
		}
	}
}
