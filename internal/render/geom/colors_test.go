package geom

import (
	"image/color"
	"testing"
)

func TestStraightColorMatchesStandardModel(t *testing.T) {
	for alpha := 1; alpha < 256; alpha++ {
		for component := 0; component < 256; component++ {
			got := straightColor(color.RGBA{R: uint8(component), G: uint8(component), B: uint8(component), A: uint8(alpha)})
			want := color.NRGBAModel.Convert(color.RGBA{R: uint8(component), G: uint8(component), B: uint8(component), A: uint8(alpha)}).(color.NRGBA)
			if got != want {
				t.Fatalf("straightColor(%d, %d) = %v, want %v", component, alpha, got, want)
			}
		}
	}

	carrier := color.RGBA{R: 255, G: 23, B: 7}
	if got, want := straightColor(carrier), (color.NRGBA{R: 255, G: 23, B: 7}); got != want {
		t.Fatalf("透明色载体 = %v, want %v", got, want)
	}
}

func TestColorLerpPremultiplicationMatchesStandardModel(t *testing.T) {
	for alpha := 0; alpha < 256; alpha++ {
		for component := 0; component < 256; component++ {
			nrgba := color.NRGBA{R: uint8(component), G: uint8(component), B: uint8(component), A: uint8(alpha)}
			got := premultipliedColor(nrgba)
			want := color.RGBAModel.Convert(nrgba).(color.RGBA)
			if got != want {
				t.Fatalf("premultiply(%d, %d) = %v, want %v", component, alpha, got, want)
			}
		}
	}
}

func TestColorLerpDoesNotAllocate(t *testing.T) {
	first := color.RGBA{R: 20, G: 10, B: 5, A: 32}
	second := color.RGBA{R: 90, G: 120, B: 180, A: 200}
	var result color.RGBA
	allocs := testing.AllocsPerRun(100, func() {
		result = colorLerp(first, second, 0.37)
	})
	if allocs != 0 {
		t.Fatalf("colorLerp 分配了 %g 次/调用", allocs)
	}
	if result == (color.RGBA{}) {
		t.Fatal("colorLerp 返回了意外的空颜色")
	}
}

var benchmarkColor color.RGBA

func BenchmarkColorLerp(b *testing.B) {
	first := color.RGBA{R: 20, G: 10, B: 5, A: 32}
	second := color.RGBA{R: 90, G: 120, B: 180, A: 200}
	for _, benchmark := range []struct {
		name string
		lerp func(color.RGBA, color.RGBA, float64) color.RGBA
	}{
		{name: "Numeric", lerp: colorLerp},
		{name: "ColorModels", lerp: colorLerpWithModels},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchmarkColor = benchmark.lerp(first, second, 0.37)
			}
		})
	}
}

func colorLerpWithModels(c0, c1 color.RGBA, t float64) color.RGBA {
	n0 := straightColorWithModel(c0)
	n1 := straightColorWithModel(c1)
	weight := uint32(t*65535.0 + 0.5)
	n := color.NRGBA{
		R: lerp(uint32(n0.R)*0x101, uint32(n1.R)*0x101, weight),
		G: lerp(uint32(n0.G)*0x101, uint32(n1.G)*0x101, weight),
		B: lerp(uint32(n0.B)*0x101, uint32(n1.B)*0x101, weight),
		A: lerp(uint32(n0.A)*0x101, uint32(n1.A)*0x101, weight),
	}
	return color.RGBAModel.Convert(n).(color.RGBA)
}

func straightColorWithModel(c color.RGBA) color.NRGBA {
	if c.A == 0 {
		return color.NRGBA{R: c.R, G: c.G, B: c.B}
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}
