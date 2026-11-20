package pdf

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestRGBStreamPNGMatchesPixelInterfaceForColorAndAlpha(t *testing.T) {
	for _, kind := range []string{"opaque", "transparent", "paletted", "gray16", "nrgba64"} {
		t.Run(kind, func(t *testing.T) {
			img := rgbStreamPNG(t, kind)
			// 隐藏具体类型，强制走优化前逐像素 At/RGBA 的算法，独立核对舍入结果。
			pixelInterface := struct{ image.Image }{img}
			wantRGB, wantAlpha, wantHasAlpha := rgbStream(pixelInterface, img.Bounds().Size())
			rgb, alpha, hasAlpha := rgbStream(img, img.Bounds().Size())
			if !bytes.Equal(rgb, wantRGB) || !bytes.Equal(alpha, wantAlpha) || hasAlpha != wantHasAlpha {
				t.Fatal("展开 PNG 包装改变了 RGB/alpha 字节或透明标记")
			}
		})
	}
}

func TestRGBStreamPNGDoesNotAllocateColorsPerPixel(t *testing.T) {
	img := rgbStreamPNG(t, "transparent")
	if _, err := img.Image(); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(5, func() {
		_, _, _ = rgbStream(img, img.Bounds().Size())
	})
	// 只需分配 RGB 和 alpha 缓冲区，不能再为每个像素装箱颜色。
	if allocations > 4 {
		t.Fatalf("RGB/alpha 导出分配次数=%g，仍可能走逐像素接口", allocations)
	}
}

func BenchmarkRGBStreamPNG(b *testing.B) {
	img := rgbStreamPNG(b, "transparent")
	if _, err := img.Image(); err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"concrete", "pixel-interface"} {
		b.Run(name, func(b *testing.B) {
			var source image.Image = img
			if name == "pixel-interface" {
				source = struct{ image.Image }{img}
			}
			b.ReportAllocs()
			b.SetBytes(int64(img.Width * img.Height * 4))
			for range b.N {
				_, _, _ = rgbStream(source, source.Bounds().Size())
			}
		})
	}
}

func rgbStreamPNG(tb testing.TB, kind string) *cimage.Image {
	tb.Helper()
	bounds := image.Rect(0, 0, 32, 24)
	var source draw.Image
	switch kind {
	case "opaque":
		source = image.NewRGBA(bounds)
	case "transparent":
		source = image.NewNRGBA(bounds)
	case "paletted":
		source = image.NewPaletted(bounds, color.Palette{
			color.NRGBA{R: 240, G: 80, B: 40, A: 255},
			color.NRGBA{R: 240, G: 80, B: 40, A: 128},
			color.Transparent,
		})
	case "gray16":
		source = image.NewGray16(bounds)
	case "nrgba64":
		source = image.NewNRGBA64(bounds)
	default:
		tb.Fatalf("未知测试图片类型：%s", kind)
	}
	alpha := []uint8{0, 1, 127, 128, 254, 255}
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			a := alpha[(x+y)%len(alpha)]
			if kind == "opaque" {
				a = 255
			}
			source.Set(x, y, color.NRGBA64{R: uint16(x)*2001 + 1, G: uint16(y)*2501 + 7, B: 0xcafe, A: uint16(a) * 257})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		tb.Fatal(err)
	}
	img, err := cimage.NewPNGImage(&encoded)
	if err != nil {
		tb.Fatal(err)
	}
	return img
}
