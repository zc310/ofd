package geom

import (
	"math"
	"testing"
)

func TestClipContainsRectToleratesOnlyOneFloatStep(t *testing.T) {
	clip := Rectangle(100, 20).Translate(147.472, 83.7063)
	box := clip.Bounds()
	for _, side := range []string{"左", "下", "右", "上"} {
		t.Run(side, func(t *testing.T) {
			bounds := box
			var value *float64
			direction := math.Inf(-1)
			switch side {
			case "左":
				value = &bounds.X0
			case "下":
				value = &bounds.Y0
			case "右":
				value, direction = &bounds.X1, math.Inf(1)
			case "上":
				value, direction = &bounds.Y1, math.Inf(1)
			}
			*value = math.Nextafter(*value, direction)
			if !ClipContainsRect(clip, bounds) {
				t.Fatal("矩形重建的单步浮点舍入不应触发轮廓求交")
			}
			*value = math.Nextafter(*value, direction)
			if ClipContainsRect(clip, bounds) {
				t.Fatal("不能把浮点舍入容差继续放宽")
			}
		})
	}
}

func TestClipContainsRectStillRejectsOverflowAndHoles(t *testing.T) {
	for _, tc := range []struct {
		name string
		clip *Path
		box  Rect
	}{
		{"真实越界", Rectangle(20, 20), Rect{X0: 1, Y0: 1, X1: 20.000001, Y1: 19}},
		{"带孔洞", Rectangle(20, 20).Append(Rectangle(5, 5).Translate(5, 5)).Settle(EvenOdd), Rect{X0: 1, Y0: 1, X1: 19, Y1: 19}},
		{"非矩形", Circle(10).Translate(10, 10), Rect{X0: 1, Y0: 1, X1: 19, Y1: 19}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ClipContainsRect(tc.clip, tc.box) {
				t.Fatal("裁剪区不完整覆盖时不能走矩形快路径")
			}
		})
	}
}
