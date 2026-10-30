package canvas

import (
	"image"
	"testing"

	"github.com/zc310/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

// 对比大裁剪框与文字交集范围，单独测量裁剪掩码的分配开销。
func BenchmarkTextClipMaskBounds(b *testing.B) {
	resolution := canvas.DPI(150)
	target := image.NewRGBA(image.Rect(0, 0, 1240, 1754))
	clip := canvas.Rectangle(210, 297).Append(canvas.Circle(10).Translate(100, 150)).Settle(canvas.EvenOdd)
	path := canvas.Rectangle(60, 4).Translate(2, 20)
	for _, tc := range []struct {
		name   string
		bounds canvas.Rect
	}{
		{"clip", clip.Bounds()},
		{"span", clip.Bounds().And(path.Bounds())},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				mask := newTextClipMask(target, clip, tc.bounds, false, resolution)
				mask.render(path, canvas.DefaultStyle, canvas.Identity, resolution)
			}
		})
	}
}

func BenchmarkClippedTextRaster(b *testing.B) {
	family := canvas.NewFontFamily("clip-benchmark")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		b.Fatal(err)
	}
	rich := canvas.NewRichText(family.Face(24, canvas.Black))
	for i := range 16 {
		fill := canvas.Black
		if i%2 == 0 {
			fill = canvas.Red
		}
		rich.WriteFace(family.Face(24, fill), "Clipping OOO ")
	}
	text := rich.ToText(155, 0, canvas.Left, canvas.Top, nil)
	for _, tc := range []struct {
		name string
		clip *canvas.Path
	}{
		{"contained", canvas.Rectangle(160, 80).Translate(-2, -75)},
		{"partial", canvas.Rectangle(100, 14).Translate(0, -18)},
		{"hole", canvas.Rectangle(160, 80).Translate(-2, -75).Append(canvas.Circle(18).Translate(50, -20)).Settle(canvas.EvenOdd)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			page := canvas.New(160, 80)
			page.RenderClippedText(text, canvas.Identity.Translate(2, 77), tc.clip)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				rasterize(page, canvas.DPI(150), canvas.DefaultColorSpace, nil)
			}
		})
	}
}
