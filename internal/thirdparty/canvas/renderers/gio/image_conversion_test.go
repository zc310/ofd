package gio

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"testing"
)

// 逐像素对照原有公式，覆盖全部 8 位色值与透明度组合以及非零起点和 16 位输入。
func TestImageConversionLookupMatchesOriginalFormula(t *testing.T) {
	nrgba := image.NewNRGBA(image.Rect(5, 7, 261, 263))
	rgba := image.NewRGBA(image.Rect(5, 7, 261, 263))
	for alpha := range 256 {
		for value := range 256 {
			c := color.NRGBA{R: uint8(value), G: uint8(255 - value), B: uint8(value / 2), A: uint8(alpha)}
			nrgba.SetNRGBA(value+5, alpha+7, c)
			rgba.Set(value+5, alpha+7, c)
		}
	}
	highDepth := image.NewNRGBA64(image.Rect(3, 5, 5, 7))
	highDepth.SetNRGBA64(3, 5, color.NRGBA64{R: 65535, G: 12000, A: 128})
	highDepth.SetNRGBA64(4, 5, color.NRGBA64{R: 64000, B: 30000, A: 32769})
	highDepth.SetNRGBA64(3, 6, color.NRGBA64{G: 65535, A: 65535})
	for _, src := range []image.Image{nrgba, rgba, highDepth, rgba.SubImage(image.Rect(10, 20, 40, 50))} {
		want := originalImageConversion(src)
		got := imageForGio(src)
		if got.Bounds() != want.Bounds() {
			t.Fatalf("输出尺寸不一致: got=%v want=%v", got.Bounds(), want.Bounds())
		}
		for y := range got.Bounds().Dy() {
			for x := range got.Bounds().Dx() {
				if a, b := got.At(x, y), want.At(x, y); a != b {
					t.Fatalf("%T 像素 (%d,%d) 不一致: got=%v want=%v", src, x, y, a, b)
				}
			}
		}
	}
}

func TestImageFastPathsPreserveStridedSubimagesAndOpaqueFormats(t *testing.T) {
	bounds := image.Rect(-7, 3, 5, 11)
	rgba := image.NewRGBA(bounds)
	nrgba := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBA{R: uint8(20 * (x - bounds.Min.X)), G: 160, B: 90, A: uint8(30 * (y - bounds.Min.Y))}
			rgba.Set(x, y, c)
			nrgba.SetNRGBA(x, y, c)
		}
	}
	rgbaBefore, nrgbaBefore := bytes.Clone(rgba.Pix), bytes.Clone(nrgba.Pix)
	sub := image.Rect(-4, 5, 3, 9)
	for _, src := range []image.Image{
		rgba.SubImage(sub), nrgba.SubImage(sub),
		image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420).SubImage(sub),
		image.NewGray(bounds).SubImage(sub), image.NewGray16(bounds).SubImage(sub),
		image.NewRGBA(image.Rect(0, 0, 2, 2)), image.NewNRGBA(image.Rect(0, 0, 2, 2)),
	} {
		got, want := imageForGio(src), originalImageConversion(src)
		if got.Bounds() != want.Bounds() {
			t.Fatalf("%T 图片起点未归零: got=%v want=%v", src, got.Bounds(), want.Bounds())
		}
		for y := range got.Bounds().Dy() {
			for x := range got.Bounds().Dx() {
				a, _ := color.RGBAModel.Convert(got.At(x, y)).(color.RGBA)
				if b := want.RGBAAt(x, y); a != b {
					t.Fatalf("%T 子图像素 (%d,%d) 转换不等价: got=%v want=%v", src, x, y, a, b)
				}
			}
		}
	}
	if !bytes.Equal(rgba.Pix, rgbaBefore) || !bytes.Equal(nrgba.Pix, nrgbaBefore) {
		t.Fatal("图片快路径修改了共享输入")
	}
}

func TestImagePreparationObservesMutationBetweenCalls(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	first := imageForGio(src)
	src.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 128})
	second := imageForGio(src)
	if a, b := first.At(0, 0), second.At(0, 0); a == b {
		t.Fatalf("源图变化后复用了旧转换结果: first=%v second=%v", a, b)
	}
	if got := src.NRGBAAt(0, 0); got != (color.NRGBA{G: 255, A: 128}) {
		t.Errorf("转换破坏源图像素: %v", got)
	}
}

func TestImageConversionConcurrentSharedInput(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), B: uint8(y * 8), A: uint8(1 + x + y)})
		}
	}
	want := originalImageConversion(src)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			got := imageForGio(src)
			for y := range 32 {
				for x := range 32 {
					if got.At(x, y) != want.At(x, y) {
						t.Errorf("并发转换像素 (%d,%d) 不一致", x, y)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

func originalImageConversion(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rectangle{Max: bounds.Size()})
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := src.At(x, y).RGBA()
			if alpha == 0 || alpha == 0xffff {
				continue
			}
			c, _ := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			encode := func(value uint8) uint8 {
				v := float64(value) / 255
				if v <= 0.04045 {
					v /= 12.92
				} else {
					v = math.Pow((v+0.055)/1.055, 2.4)
				}
				v *= float64(c.A) / 255
				if v <= 0.0031308 {
					v *= 12.92
				} else {
					v = 1.055*math.Pow(v, 1/2.4) - 0.055
				}
				return uint8(math.Round(v * 255))
			}
			dst.SetRGBA(x-bounds.Min.X, y-bounds.Min.Y, color.RGBA{R: encode(c.R), G: encode(c.G), B: encode(c.B), A: c.A})
		}
	}
	return dst
}

func BenchmarkImagePreparationFormats(b *testing.B) {
	const size = 512
	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	nrgba := image.NewNRGBA(rgba.Bounds())
	opaque := image.NewRGBA(rgba.Bounds())
	for y := range size {
		for x := range size {
			c := color.NRGBA{R: 200, G: 120, B: 60, A: uint8(1 + (x+y)%254)}
			rgba.Set(x, y, c)
			nrgba.SetNRGBA(x, y, c)
			opaque.SetRGBA(x, y, color.RGBA{R: 200, G: 120, B: 60, A: 255})
		}
	}
	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"RGBA透明", rgba},
		{"NRGBA透明", nrgba},
		{"RGBA不透明", opaque},
		{"YCbCr不透明", image.NewYCbCr(rgba.Bounds(), image.YCbCrSubsampleRatio420)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = imageForGio(tc.img)
			}
		})
	}
}
