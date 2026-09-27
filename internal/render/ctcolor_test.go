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

// TestPremultipliedKeepsRGBAtZeroAlpha 保护完全透明色的原始 RGB 载体：
// a == 0 时若把分量压成全 0，渐变色标插值会丢掉整段颜色（透明红→蓝只剩蓝）；
// 同时混合端必须仍按全透明处理，不能因载体非 0 而可见。
func TestPremultipliedKeepsRGBAtZeroAlpha(t *testing.T) {
	got := premultiplied(255, 0, 0, 0)
	if got != (color.RGBA{R: 255, A: 0}) {
		t.Fatalf("premultiplied(255,0,0,0) = %v, want {255 0 0 0}", got)
	}
	if n := color.NRGBAModel.Convert(got).(color.NRGBA); n != (color.NRGBA{}) {
		t.Fatalf("NRGBA = %v, want 全透明 {0 0 0 0}", n)
	}
}
