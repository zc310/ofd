package canvas

import (
	"testing"

	"github.com/zc310/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

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
				rasterize(page, canvas.DPI(150), canvas.DefaultColorSpace)
			}
		})
	}
}
