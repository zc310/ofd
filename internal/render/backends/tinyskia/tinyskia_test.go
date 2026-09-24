package tinyskia_test

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/render/backends/tinyskia"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestTinySkiaNonPlainGradientNotTransparent 回归：非平铺 geom 渐变
// （无法映射到 tinyskia 原生 Linear/RadialGradient 的类型）此前会退化为
// 透明底色；现在应逐像素采样到 pattern，输出与 geom.Gradient.At 语义一致。
func TestTinySkiaNonPlainGradientNotTransparent(t *testing.T) {
	b, err := tinyskia.New(100, 100, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	grad := geom.GradientFunc(func(x, y float64) color.RGBA {
		t := x / 100.0
		return color.RGBA{uint8(0xff - 0xdf*t), 0x20, uint8(0x20 + 0xdf*t), 0xff}
	})
	b.SetFillGradient(grad)
	b.DrawPath(0, 0, geom.Rectangle(100, 100))

	img := b.Raster()
	wpx, hpx := img.Bounds().Max.X, img.Bounds().Max.Y
	dpmm := 96.0 / 25.4
	for _, xPx := range []int{wpx / 8, wpx / 2, wpx - wpx/8} {
		y := hpx / 2
		i := y*img.Stride + xPx*4
		got := color.RGBA{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2], A: img.Pix[i+3]}
		if got.A == 0 && got.R == 0 && got.G == 0 && got.B == 0 {
			t.Fatalf("非纯渐变像素 (%d,%d) 退化为透明: %v", xPx, y, got)
		}
		want := grad.At(float64(xPx)/dpmm, (float64(hpx)-float64(y))/dpmm)
		for name, pair := range map[string][2]uint8{"R": {got.R, want.R}, "G": {got.G, want.G}, "B": {got.B, want.B}} {
			d := int(pair[0]) - int(pair[1])
			if d < 0 {
				d = -d
			}
			if d > 2 {
				t.Fatalf("非纯渐变像素 (%d,%d) %s = %d, want %d (%v)", xPx, y, name, pair[0], pair[1], want)
			}
		}
	}
}

// TestTinySkiaNonPlainGradientStroke 回归：描边使用非纯渐变也应经 pattern
// 逐像素采样而非透明。
func TestTinySkiaNonPlainGradientStroke(t *testing.T) {
	b, err := tinyskia.New(100, 100, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	grad := geom.GradientFunc(func(x, y float64) color.RGBA {
		return color.RGBA{R: 0x20, G: 0x20, B: 0xff, A: 0xff}
	})
	b.SetStrokeWidth(20)
	b.SetStrokeGradient(grad)
	b.DrawPath(0, 0, geom.Rectangle(70, 70).Translate(15, 15))

	img := b.Raster()
	wpx, hpx := img.Bounds().Max.X, img.Bounds().Max.Y
	dpmm := 96.0 / 25.4
	mid := int(15 * dpmm)
	for _, xPx := range []int{mid, wpx - mid} {
		y := hpx / 2
		i := y*img.Stride + xPx*4
		if img.Pix[i+2] == 0 && img.Pix[i] == 0 && img.Pix[i+1] == 0 {
			t.Fatalf("描边非纯渐变像素 (%d,%d) 为透明/黑: rgba(%d,%d,%d,%d)",
				xPx, y, img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3])
		}
		if img.Pix[i+2] < 200 {
			t.Fatalf("描边非纯渐变像素 (%d,%d) B = %d, want 蓝色系", xPx, y, img.Pix[i+2])
		}
	}
}
