package imageops

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestContentBoundsAndCropKeepSubimageOriginsAndSourcePixels(t *testing.T) {
	for _, origin := range []image.Point{{}, {X: 10, Y: 20}, {X: -8, Y: -6}} {
		src := image.NewNRGBA(image.Rect(origin.X, origin.Y, origin.X+12, origin.Y+10))
		src.SetNRGBA(origin.X, origin.Y, color.NRGBA{A: 8})
		src.SetNRGBA(origin.X+2, origin.Y+3, color.NRGBA{R: 255, A: 9})
		src.SetNRGBA(origin.X+7, origin.Y+6, color.NRGBA{B: 255, A: 255})
		before := bytes.Clone(src.Pix)
		sub := src.SubImage(image.Rect(origin.X+1, origin.Y+2, origin.X+11, origin.Y+9))
		for _, img := range []image.Image{src, sub, genericAlphaImage{sub}} {
			x0, y0, x1, y1 := ContentBounds(img)
			if x0 != float64(origin.X+2) || y0 != float64(origin.Y+3) || x1 != float64(origin.X+8) || y1 != float64(origin.Y+7) {
				t.Fatalf("内容边界错误：%g,%g,%g,%g", x0, y0, x1, y1)
			}
			cropped := Crop(img, int(x0), int(y0), int(x1), int(y1))
			if cropped.Bounds() != image.Rect(0, 0, 6, 4) {
				t.Fatalf("裁后范围=%v", cropped.Bounds())
			}
			for y := range 4 {
				for x := range 6 {
					got := color.RGBAModel.Convert(cropped.At(x, y))
					want := color.RGBAModel.Convert(img.At(int(x0)+x, int(y0)+y))
					if got != want {
						t.Fatalf("裁图像素(%d,%d)=%v，期望%v", x, y, got, want)
					}
				}
			}
		}
		if !bytes.Equal(before, src.Pix) {
			t.Fatal("测量或裁图修改了源像素")
		}
	}
}

func TestResourceMaskScalesOffsetsAndPreservesSource(t *testing.T) {
	src := image.NewNRGBA(image.Rect(-4, 5, 0, 7))
	for y := 5; y < 7; y++ {
		for x := -4; x < 0; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
		}
	}
	mask := image.NewNRGBA(image.Rect(10, 20, 12, 21))
	mask.SetNRGBA(10, 20, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	mask.SetNRGBA(11, 20, color.NRGBA{R: 255, G: 255, B: 255, A: 0})
	before, maskBefore := bytes.Clone(src.Pix), bytes.Clone(mask.Pix)
	got := ApplyResourceMask(src, mask)
	if got.Bounds() != src.Bounds() {
		t.Fatal("资源掩码改变了图像原点")
	}
	for y := 5; y < 7; y++ {
		for x := -4; x < 0; x++ {
			want := color.NRGBA{R: 200, G: 100, B: 50, A: 128}
			if x >= -2 {
				want.A = 0
			}
			if pixel := color.NRGBAModel.Convert(got.At(x, y)); pixel != want {
				t.Fatalf("资源掩码像素(%d,%d)=%v，期望%v", x, y, pixel, want)
			}
		}
	}
	if !bytes.Equal(before, src.Pix) || !bytes.Equal(maskBefore, mask.Pix) {
		t.Fatal("资源掩码修改了共享输入")
	}
	if ApplyResourceMask(src, nil) != src || ApplyAlpha(src, 255) != src {
		t.Fatal("无需处理时丢失原图复用")
	}
}
