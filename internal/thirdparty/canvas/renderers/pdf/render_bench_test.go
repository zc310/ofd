package pdf

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func benchmarkPDFPath() *canvas.Path {
	p := &canvas.Path{}
	for i := range 200 {
		x := float64(i) * 0.7
		y := float64(i%17) * 0.9
		p.MoveTo(x, y)
		p.LineTo(x+0.3, y+0.4)
		p.CubeTo(x+0.5, y+0.1, x+0.8, y+0.6, x+1.0, y+0.2)
		p.Close()
	}
	return p
}

func BenchmarkPDFRenderPaths(b *testing.B) {
	path := benchmarkPDFPath()
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: color.RGBA{R: 30, G: 60, B: 90, A: 255}}
	style.Stroke = canvas.Paint{Color: color.RGBA{R: 200, G: 10, B: 10, A: 255}}
	style.StrokeWidth = 0.2
	for _, compress := range []bool{false, true} {
		b.Run(map[bool]string{false: "nocompress", true: "compress"}[compress], func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				var out bytes.Buffer
				r := New(&out, 210, 297, &Options{Compress: compress})
				for i := range 20 {
					r.RenderPath(path, style, canvas.Identity.Translate(float64(i), float64(i)))
				}
				if err := r.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPDFRenderImages(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = byte(i)
		img.Pix[i+1] = byte(i >> 8)
		img.Pix[i+2] = byte(i >> 16)
		img.Pix[i+3] = 255
	}
	b.ReportAllocs()
	for range b.N {
		var out bytes.Buffer
		r := New(&out, 210, 297, &Options{Compress: true})
		for i := range 4 {
			r.RenderImage(img, canvas.Identity.Translate(float64(i), float64(i)))
		}
		if err := r.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPDFGradientFunctions(b *testing.B) {
	grad := canvas.Grad{
		{Offset: 0, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 0.5, Color: color.RGBA{G: 255, A: 255}},
		{Offset: 1, Color: color.RGBA{B: 255, A: 255}},
	}
	writer := newPDFWriter(&bytes.Buffer{})
	b.ReportAllocs()
	for range b.N {
		writer.gradientFunction(grad)
	}
}

func BenchmarkDecString(b *testing.B) {
	values := []float64{0, 1, -1, 0.1, 0.123456789, 123.456789, 123456789.123, math.MaxInt32 + 1, math.SmallestNonzeroFloat64}
	b.ReportAllocs()
	for range b.N {
		for _, v := range values {
			_ = dec(v).String()
		}
	}
}

func BenchmarkPDFRenderText(b *testing.B) {
	family := canvas.NewFontFamily("bench")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		b.Fatal(err)
	}
	face := family.Face(10, color.RGBA{A: 255})
	line := canvas.NewTextLine(face, strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20), canvas.Left)
	b.ReportAllocs()
	for range b.N {
		var out bytes.Buffer
		r := New(&out, 210, 297, &Options{Compress: false})
		for i := range 10 {
			r.RenderText(line, canvas.Identity.Translate(10, float64(i*10+10)))
		}
		if err := r.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
