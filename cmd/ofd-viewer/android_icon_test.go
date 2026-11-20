package main

import (
	"image/png"
	"os"
	"testing"
)

// Android 自适应图标会裁切外围，背景区域不能出现图案或文字。
func TestAndroidIconKeepsOuterSafetyMargin(t *testing.T) {
	f, err := os.Open("android-icon.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 800 || bounds.Dy() != 800 {
		t.Fatalf("Android 图标尺寸不符合预期: %v", bounds)
	}
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			if x >= 136 && x < 664 && y >= 136 && y < 664 {
				continue
			}
			r, g, b, a := img.At(x, y).RGBA()
			if r != 0xfafa || g != 0xfafa || b != 0xfafa || a != 0xffff {
				t.Fatalf("图标安全边距不为背景色: (%d, %d)", x, y)
			}
		}
	}
}
