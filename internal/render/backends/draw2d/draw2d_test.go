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

// TestDraw2DGradientFillNotBlack 回归：draw2d 后端此前把渐变近似成不透明黑
// （gradientColor.RGBA 固定返回 A=0xff 的黑）。现在应逐像素采样，输出与
// geom.Gradient.At 的设备像素语义一致，且不被黑色污染。
func TestDraw2DGradientFillNotBlack(t *testing.T) {
	b, err := draw2d.New(100, 100, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	grad := geom.NewLinearGradient(geom.Point{X: 0, Y: 0}, geom.Point{X: 100, Y: 0})
	grad.Add(0, color.RGBA{R: 0xff, G: 0x20, B: 0x20, A: 0xff})
	grad.Add(1, color.RGBA{R: 0x20, G: 0x20, B: 0xff, A: 0xff})
	b.SetFillGradient(grad)
	b.DrawPath(0, 0, geom.Rectangle(100, 100))

	img := b.Raster()
	wpx, hpx := img.Bounds().Max.X, img.Bounds().Max.Y
	dpmm := 96.0 / 25.4
	hpix := float64(hpx)
	sample := func(xPx, yPx int) (color.RGBA, color.RGBA) {
		x := xPx
		y := yPx
		i := y*img.Stride + x*4
		got := color.RGBA{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2], A: img.Pix[i+3]}
		want := grad.At(float64(x)/dpmm, (hpix-float64(y))/dpmm)
		return got, want
	}
	// 中排左、中、右各取一个避开抗锯齿边缘的内部像素。
	checks := [][2]int{{img.Bounds().Max.X / 8, hpx / 2}, {wpx / 2, hpx / 2}, {wpx - wpx/8, hpx / 2}}
	for _, c := range checks {
		got, want := sample(c[0], c[1])
		if got.R == 0 && got.G == 0 && got.B == 0 {
			t.Fatalf("渐变像素 (%d,%d) 为黑色: %v", c[0], c[1], got)
		}
		for name, pair := range map[string][2]uint8{"R": {got.R, want.R}, "G": {got.G, want.G}, "B": {got.B, want.B}} {
			d := int(pair[0]) - int(pair[1])
			if d < 0 {
				d = -d
			}
			if d > 3 {
				t.Fatalf("渐变像素 (%d,%d) %s = %d, want %d (%v)", c[0], c[1], name, pair[0], pair[1], want)
			}
		}
	}
	// 渐变方向验证：左红右蓝。
	left, _ := sample(wpx/8, hpx/2)
	right, _ := sample(wpx-wpx/8, hpx/2)
	if !(left.R > left.B && right.B > right.R) {
		t.Fatalf("渐变方向异常: left=%v right=%v", left, right)
	}
}

// TestDraw2DGradientStrokeNotBlack 回归：描边渐变同样逐像素采样而非黑色。
func TestDraw2DGradientStrokeNotBlack(t *testing.T) {
	b, err := draw2d.New(100, 100, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	g := geom.NewLinearGradient(geom.Point{X: 0, Y: 0}, geom.Point{X: 100, Y: 0})
	g.Add(0, color.RGBA{R: 0xff, G: 0x20, B: 0x20, A: 0xff})
	g.Add(1, color.RGBA{R: 0x20, G: 0x20, B: 0xff, A: 0xff})
	b.SetStrokeWidth(20)
	b.SetStrokeGradient(g)
	// 描边一个矩形并填充内部透明，确保采样点落在描边带上。
	b.DrawPath(0, 0, geom.Rectangle(70, 70).Translate(15, 15))

	img := b.Raster()
	wpx, hpx := img.Bounds().Max.X, img.Bounds().Max.Y
	dpmm := 96.0 / 25.4
	// 矩形 15mm 处左右边、描边宽 20mm，取两条描边带的中心线采样
	// （满覆盖，避开抗锯齿过渡像素）。
	samples := []int{int(15 * dpmm), wpx - int(15*dpmm)}
	for _, xPx := range samples {
		y := hpx / 2
		i := y*img.Stride + xPx*4
		c := color.RGBA{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2], A: img.Pix[i+3]}
		if c.R == 0 && c.G == 0 && c.B == 0 {
			t.Fatalf("描边渐变像素 (%d,%d) 为黑色: %v", xPx, y, c)
		}
		want := g.At(float64(xPx)/dpmm, (float64(hpx)-float64(y))/dpmm)
		for name, pair := range map[string][2]uint8{"R": {c.R, want.R}, "G": {c.G, want.G}, "B": {c.B, want.B}} {
			d := int(pair[0]) - int(pair[1])
			if d < 0 {
				d = -d
			}
			if d > 8 {
				t.Fatalf("描边渐变像素 (%d,%d) %s = %d, want %d (%v)", xPx, y, name, pair[0], pair[1], want)
			}
		}
	}
}
