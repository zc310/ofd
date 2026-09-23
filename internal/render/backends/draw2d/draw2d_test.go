package draw2d_test

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/render/backends/draw2d"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestDraw2DBackendRendersContent 冒烟测试：draw2d 后端能按 DrawContext
// 绘制路径与图片并产出非空 RGBA。
func TestDraw2DBackendRendersContent(t *testing.T) {
	b, err := draw2d.New(100, 100, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	b.SetFillColor(color.White)
	b.DrawPath(0, 0, geom.Rectangle(100, 100))
	b.SetFillColor(color.RGBA{R: 0xd3, G: 0x2f, B: 0x2f, A: 0xff})
	b.DrawPath(10, 10, geom.Circle(30).Translate(50, 50))
	b.SetStrokeColor(color.RGBA{R: 0x15, G: 0x65, B: 0xc0, A: 0xff})
	b.SetStrokeWidth(2)
	b.DrawPath(0, 0, geom.Rectangle(80, 80).Translate(10, 10))

	img := b.Raster()
	if img == nil || img.Bounds().Empty() {
		t.Fatal("draw2d 输出为空")
	}
	nonWhite := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff || img.Pix[i+1] != 0xff || img.Pix[i+2] != 0xff {
			nonWhite++
		}
	}
	if nonWhite == 0 {
		t.Fatal("draw2d 未绘制任何非白像素")
	}
}
