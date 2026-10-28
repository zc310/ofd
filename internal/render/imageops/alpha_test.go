package imageops

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/render/imageasset"
)

// referenceImageAlpha 保留通用逐像素实现，用于验证各种像素格式、原点和步长。
func referenceImageAlpha(img image.Image, alpha uint8) *image.NRGBA {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c, _ := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			c.A = uint8(int(c.A) * int(alpha) / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

type genericAlphaImage struct{ image.Image }

func alphaTestImages(t *testing.T) map[string]image.Image {
	t.Helper()
	bounds := image.Rect(-5, 3, 19, 20)
	nrgba := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			nrgba.SetNRGBA(x, y, color.NRGBA{R: uint8(x*37 + y*13), G: uint8(x*11 - y*23), B: uint8(x*7 + y*41), A: uint8((x + 5 + y - 3) * 17)})
		}
	}
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, nrgba, bounds.Min, draw.Src)
	gray := image.NewGray(bounds)
	draw.Draw(gray, bounds, nrgba, bounds.Min, draw.Src)
	crop := image.Rect(-3, 5, 14, 17)
	images := map[string]image.Image{
		"NRGBA": nrgba, "RGBA": rgba, "Gray": gray,
		"NRGBA subimage": nrgba.SubImage(crop), "RGBA subimage": rgba.SubImage(crop),
		"generic": genericAlphaImage{nrgba.SubImage(crop)},
		"empty":   image.NewNRGBA(image.Rect(3, 4, 3, 4)),
	}
	for _, ratio := range []image.YCbCrSubsampleRatio{
		image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422,
		image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440,
		image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410,
	} {
		ycc := image.NewYCbCr(bounds, ratio)
		for i := range ycc.Y {
			ycc.Y[i] = uint8(i * 31)
		}
		for i := range ycc.Cb {
			ycc.Cb[i], ycc.Cr[i] = uint8(i*17), uint8(i*43)
		}
		images[fmt.Sprintf("YCbCr %v", ratio)] = ycc
		images[fmt.Sprintf("YCbCr subimage %v", ratio)] = ycc.SubImage(crop)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, nrgba); err != nil {
		t.Fatal(err)
	}
	images["encoded PNG"] = imageasset.NewEncodedImage("png", append([]byte(nil), encoded.Bytes()...))
	encoded.Reset()
	if err := jpeg.Encode(&encoded, rgba, nil); err != nil {
		t.Fatal(err)
	}
	images["encoded JPEG"] = imageasset.NewEncodedImage("jpeg", append([]byte(nil), encoded.Bytes()...))
	return images
}

func TestImageAlphaMatchesReferenceForFormatsAndSubimages(t *testing.T) {
	for name, img := range alphaTestImages(t) {
		for _, alpha := range []uint8{0, 1, 51, 128, 254, 255} {
			t.Run(fmt.Sprintf("%s/%d", name, alpha), func(t *testing.T) {
				want := referenceImageAlpha(img, alpha)
				got := ApplyAlpha(img, alpha)
				if got.Bounds() != want.Bounds() {
					t.Fatalf("bounds=%v, want %v", got.Bounds(), want.Bounds())
				}
				for y := want.Rect.Min.Y; y < want.Rect.Max.Y; y++ {
					for x := want.Rect.Min.X; x < want.Rect.Max.X; x++ {
						pixel, _ := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
						if pixel != want.NRGBAAt(x, y) {
							t.Fatalf("pixel (%d,%d)=%v, want %v", x, y, pixel, want.NRGBAAt(x, y))
						}
					}
				}
			})
		}
	}
}

func TestImageAlphaDoesNotMutateSource(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 12, 10))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 17)
	}
	before := append([]byte(nil), src.Pix...)
	sub := src.SubImage(image.Rect(3, 2, 9, 8))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _ = ApplyAlpha(sub, 128) })
	}
	wg.Wait()
	if !bytes.Equal(src.Pix, before) {
		t.Fatal("alpha conversion mutated source")
	}
}

func TestImageAlphaRGBAUnpremultiplicationMatchesAllByteValues(t *testing.T) {
	// 覆盖全部合法的预乘分量与 alpha 组合，保护整数除法与截断精度。
	src := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for a := range 256 {
		for value := range a + 1 {
			src.SetRGBA(value, a, color.RGBA{R: uint8(value), G: uint8(a - value), B: uint8(value / 2), A: uint8(a)})
		}
	}
	want := referenceImageAlpha(src, 128)
	got, ok := ApplyAlpha(src, 128).(*image.NRGBA)
	if !ok {
		t.Fatal("透明度合成没有返回 NRGBA")
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("RGBA unpremultiplication differs from color.NRGBAModel")
	}
}

func BenchmarkImageAlpha(b *testing.B) {
	const size = 1024
	nrgba := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := range nrgba.Pix {
		nrgba.Pix[i] = uint8(i * 17)
	}
	rgba := image.NewRGBA(nrgba.Rect)
	draw.Draw(rgba, rgba.Rect, nrgba, nrgba.Rect.Min, draw.Src)
	ycc := image.NewYCbCr(nrgba.Rect, image.YCbCrSubsampleRatio420)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, rgba, nil); err != nil {
		b.Fatal(err)
	}
	lazy := imageasset.NewEncodedImage("jpeg", encoded.Bytes())
	if _, err := lazy.Image(); err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"NRGBA", nrgba}, {"RGBA", rgba}, {"YCbCr", ycc}, {"encoded JPEG", lazy},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(size * size * 4)
			for b.Loop() {
				_ = ApplyAlpha(tc.img, 128)
			}
		})
	}
}
