package imageops

import (
	"image"
	"image/color"
	"testing"
)

// 不透明印章去除纸张白底，保留墨迹及已有透明度，不修改输入图像。
func TestSealOpaqueWhiteBackgroundBecomesTransparent(t *testing.T) {
	pal := color.Palette{color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{R: 223, G: 25, B: 29, A: 255}}
	src := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	copy(src.Pix, []uint8{0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0})
	got := SealTransparentBackground(src)
	if _, _, _, a := got.At(0, 0).RGBA(); a != 0 {
		t.Fatal("白色背景未透明化")
	}
	for _, point := range []image.Point{{X: 1}, {X: 1, Y: 1}} {
		if got.At(point.X, point.Y) != color.NRGBAModel.Convert(src.At(point.X, point.Y)) {
			t.Fatal("印章墨迹颜色或透明度被修改")
		}
	}
	if _, _, _, a := src.At(0, 0).RGBA(); a != 0xffff {
		t.Fatal("源图被修改")
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	transparent.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 128})
	if replaced := SealTransparentBackground(transparent); replaced != image.Image(transparent) {
		t.Fatal("已有透明像素的印章应直接复用")
	}
}

type imageWithoutOpacity struct{ image.Image }

func TestSealBackgroundKeepsThresholdAndNonzeroOrigin(t *testing.T) {
	src := image.NewNRGBA(image.Rect(5, 7, 8, 8))
	src.SetNRGBA(5, 7, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	src.SetNRGBA(6, 7, color.NRGBA{R: 249, G: 255, B: 255, A: 255})
	src.SetNRGBA(7, 7, color.NRGBA{R: 223, G: 25, B: 29, A: 255})
	got := SealTransparentBackground(src)
	if got.Bounds() != src.Bounds() {
		t.Fatal("白底处理改变了图像原点")
	}
	if _, _, _, a := got.At(5, 7).RGBA(); a != 0 {
		t.Fatal("阈值白色没有透明化")
	}
	for x := 6; x < 8; x++ {
		if got.At(x, 7) != src.At(x, 7) {
			t.Fatal("低于白底阈值的像素被修改")
		}
	}
	wrapped := imageWithoutOpacity{Image: src}
	if SealTransparentBackground(wrapped) != wrapped {
		t.Fatal("无法查询不透明度的图片应直接复用")
	}
}

func TestCropNRGBAKeepsStraightColorAndIndependentPixels(t *testing.T) {
	src := image.NewNRGBA(image.Rect(5, 7, 9, 11))
	want := color.NRGBA{R: 201, G: 53, B: 27, A: 93}
	src.SetNRGBA(7, 8, want)
	got, ok := CropNRGBA(src, image.Rect(7, 8, 9, 10)).(*image.NRGBA)
	if !ok || got.Bounds() != image.Rect(0, 0, 2, 2) || got.NRGBAAt(0, 0) != want {
		t.Fatalf("裁剪丢失原点映射或非预乘颜色: %v", got)
	}
	got.SetNRGBA(0, 0, color.NRGBA{})
	if src.NRGBAAt(7, 8) != want {
		t.Fatal("裁剪图片与源图共享了可变像素")
	}
}
