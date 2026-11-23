package rastercore

import (
	"image"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

type coordinateHooks struct {
	Hooks

	path   []DeviceSeg
	matrix geom.Matrix
}

func (h *coordinateHooks) DrawDevicePath(path *DevicePath, _ geom.Matrix, _, _ bool, _, _ geom.Gradient, _ StrokeStyle) {
	h.path = append(h.path[:0], path.Segs...)
}

func (h *coordinateHooks) RenderImage(_ image.Image, matrix geom.Matrix) {
	h.matrix = matrix
}

func TestCoreMapsTopLeftPageAndImageAtBackendBoundary(t *testing.T) {
	hooks := &coordinateHooks{}
	core, err := New(30, 40, geom.DPMM(4), func(int, int, float64, float64) Hooks { return hooks })
	if err != nil {
		t.Fatal(err)
	}
	core.DrawPath(2, 3, geom.Rectangle(4, 5))
	if len(hooks.path) == 0 || hooks.path[0].P[0] != (geom.Point{X: 8, Y: 12}) {
		t.Fatalf("页面左上角没有映射到正确设备像素：%v", hooks.path)
	}
	core.DrawImage(image.NewRGBA(image.Rect(0, 0, 2, 4)), 5, 6, 2)
	if want := (geom.Matrix{{.5, 0, 5}, {0, .5, 32}}); !hooks.matrix.Equals(want) {
		t.Fatalf("图片左上角或源轴向错误：%v，期望 %v", hooks.matrix, want)
	}
	core.Translate(3, 7)
	absolute := geom.Identity.Translate(1, 2)
	core.RenderImage(image.NewRGBA(image.Rect(0, 0, 2, 4)), absolute)
	if hooks.matrix != absolute {
		t.Fatal("绝对图片矩阵被重复叠加当前页面变换")
	}
}
