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

// 裁剪边界已对齐像素时，字形范围的小数边界不应迫使矩形裁剪生成 Alpha 掩码。
func BenchmarkTextClipMaskRectangle(b *testing.B) {
	resolution := canvas.DPMM(5)
	target := image.NewRGBA(image.Rect(0, 0, 1200, 1600))
	path := canvas.Circle(5.13).Translate(8.17, 8.23)
	for _, tc := range []struct {
		name string
		clip *canvas.Path
	}{
		{"aligned", canvas.Rectangle(12, 10).Translate(4, 3)},
		{"fractional", canvas.Rectangle(12, 10).Translate(4.13, 3.17)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			bounds := tc.clip.Bounds().And(path.Bounds())
			b.ReportAllocs()
			for b.Loop() {
				mask := newTextClipMask(target, tc.clip, bounds, true, resolution)
				mask.render(path, canvas.DefaultStyle, canvas.Identity, resolution)
			}
		})
	}
}

// 分组只需要容纳已有 span，不应为多 span 分组额外构造一个随后丢弃的单 span 文本。
func BenchmarkClippedTextFragment(b *testing.B) {
	family := canvas.NewFontFamily("fragment-benchmark")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		b.Fatal(err)
	}
	rich := canvas.NewRichText(family.Face(20, canvas.Black))
	for range 16 {
		rich.WriteFace(family.Face(20, canvas.Black), "OO")
	}
	line := rich.ToText(0, 0, canvas.Left, canvas.Top, nil)
	var spans []canvas.TextSpan
	line.WalkLines(func(_ float64, values []canvas.TextSpan) { spans = values })
	for _, tc := range []struct {
		name  string
		count int
	}{
		{"single", 1},
		{"pair", 2},
		{"many", 16},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, ok := clippedTextFragment(line, spans[:tc.count]); !ok {
					b.Fatal("分组构造失败")
				}
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
