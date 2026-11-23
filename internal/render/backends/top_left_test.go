package backends_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

// 页面原点与图片锚点均为左上角；非对称探针防止上下镜像或二次翻转。
func TestBackendsUseTopLeftPageAndImageCoordinates(t *testing.T) {
	for _, name := range render.RegisteredBackends() {
		t.Run(name, func(t *testing.T) {
			backend, err := render.NewBackend(name, 30, 40, geom.DPMM(4))
			if err != nil {
				t.Fatal(err)
			}
			backend.SetFillColor(color.RGBA{R: 255, A: 255})
			backend.DrawPath(2, 3, geom.Rectangle(4, 5))
			backend.Push()
			backend.Translate(10, 2)
			backend.Scale(2, 2)
			backend.SetFillColor(color.RGBA{G: 255, A: 255})
			backend.DrawPath(1, 2, geom.Rectangle(2, 3))
			backend.Pop()
			source := image.NewRGBA(image.Rect(0, 0, 4, 8))
			for y := range 8 {
				c := color.RGBA{B: 255, A: 255}
				if y < 4 {
					c = color.RGBA{R: 255, G: 255, A: 255}
				}
				for x := range 4 {
					source.SetRGBA(x, y, c)
				}
			}
			backend.DrawImage(source, 20, 4, 1)
			raster := backend.Raster()
			for _, probe := range []struct {
				x, y int
				want color.RGBA
			}{
				{4, 5, color.RGBA{R: 255, A: 255}},
				{14, 8, color.RGBA{G: 255, A: 255}},
				{22, 6, color.RGBA{R: 255, G: 255, A: 255}},
				{22, 10, color.RGBA{B: 255, A: 255}},
				{4, 35, color.RGBA{}},
				{22, 30, color.RGBA{}},
			} {
				if got := raster.RGBAAt(probe.x*4, probe.y*4); got != probe.want {
					t.Errorf("页面(%d,%d)=%v，期望%v", probe.x, probe.y, got, probe.want)
				}
			}
		})
	}
}
