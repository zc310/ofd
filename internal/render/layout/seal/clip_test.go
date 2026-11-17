package seal

import (
	"image"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestSealClipBoxClamped(t *testing.T) {
	boundary := spec.StBox{X: 100, Y: 50, Width: 40, Height: 40}
	for _, tc := range []struct {
		name       string
		clip, want spec.StBox
		ok         bool
	}{
		{"缺省", spec.StBox{}, spec.StBox{}, false},
		{"零宽", spec.StBox{Height: 40}, spec.StBox{}, false},
		{"零高", spec.StBox{Width: 8}, spec.StBox{}, false},
		{"盒外", spec.StBox{X: 50, Width: 8, Height: 8}, spec.StBox{}, false},
		{"右越界", spec.StBox{X: 36, Width: 8, Height: 40}, spec.StBox{X: 136, Y: 50, Width: 4, Height: 40}, true},
		{"左越界", spec.StBox{X: -8, Width: 16, Height: 40}, spec.StBox{X: 100, Y: 50, Width: 8, Height: 40}, true},
		{"盒内", spec.StBox{X: 8, Y: 4, Width: 8, Height: 20}, spec.StBox{X: 108, Y: 54, Width: 8, Height: 20}, true},
		{"整枚", spec.StBox{Width: 40, Height: 40}, boundary, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClipBox(boundary, tc.clip)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("裁剪框=%+v,%v，期望%+v,%v", got, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := ClipBox(spec.StBox{}, spec.StBox{Width: 8, Height: 8}); ok {
		t.Fatal("零尺寸 Boundary 不应产生裁剪")
	}
}

func TestCropRectKeepsNonzeroImageOriginAndPixelRounding(t *testing.T) {
	boundary := spec.StBox{X: 100, Y: 50, Width: 40, Height: 40}
	bounds := image.Rect(5, 7, 105, 107)
	for _, tc := range []struct {
		name string
		dst  spec.StBox
		want image.Rectangle
		ok   bool
	}{
		{"右上", spec.StBox{X: 120, Y: 50, Width: 20, Height: 20}, image.Rect(55, 7, 105, 57), true},
		{"左下", spec.StBox{X: 100, Y: 70, Width: 20, Height: 20}, image.Rect(5, 57, 55, 107), true},
		{"像素取整", spec.StBox{X: 100.25, Y: 50.25, Width: 0.5, Height: 0.5}, image.Rect(6, 8, 7, 9), true},
		{"空区域", spec.StBox{X: 200, Y: 50, Width: 8, Height: 8}, image.Rectangle{}, false},
		{"整图", boundary, bounds, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CropRect(bounds, boundary, tc.dst)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("像素区域=%v,%v，期望%v,%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
