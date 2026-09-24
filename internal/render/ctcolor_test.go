package render

import (
	"image/color"
	"testing"
)

func TestScaleAlphaKeepsColorUnderPremultipliedRGBA(t *testing.T) {
	// color.RGBA 是预乘格式：浅灰 (170,160,165) 在 20% 不透明度下必须预乘成
	// (34,32,33,51)，反预乘后才能还原出原色。只改 A 会让颜色被放大成白色，
	// 使浅色半透明水印在白底上不可见。
	got := scaleAlpha(color.RGBA{R: 170, G: 160, B: 165, A: 255}, 51)
	n := color.NRGBAModel.Convert(got).(color.NRGBA)
	if n.R != 170 || n.G != 160 || n.B != 165 || n.A != 51 {
		t.Fatalf("scaleAlpha -> NRGBA %v, want {170 160 165 51}", n)
	}
	if got.A != 51 {
		t.Fatalf("scaleAlpha alpha = %d, want 51", got.A)
	}
}

func TestPremultipliedRoundTrip(t *testing.T) {
	got := premultiplied(170, 160, 165, 51)
	n := color.NRGBAModel.Convert(got).(color.NRGBA)
	if n.R != 170 || n.G != 160 || n.B != 165 || n.A != 51 {
		t.Fatalf("premultiplied -> NRGBA %v, want {170 160 165 51}", n)
	}
}
