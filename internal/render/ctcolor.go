package render

import (
	"image/color"

	"github.com/zc310/ofd/internal/render/drawing"
)

// CTColor 定义在 drawing 包；这里用别名保持本包既有调用点（updateCtColor 等）可用。
type CTColor = drawing.CTColor

// scaleAlpha 按不透明度缩放颜色。color.RGBA 是预乘格式（canvas 会反预乘还原），
// 因此不能只改 A：必须经非预乘的 NRGBA 正确换算，否则半透明的浅色会被反预乘
// 放大成白色而不可见（例如 Alpha=51 的浅灰水印会变成白色）。
func scaleAlpha(c color.RGBA, opacity uint8) color.RGBA {
	if opacity >= 255 {
		return c
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = uint8(uint16(n.A) * uint16(opacity) / 255)
	return color.RGBAModel.Convert(n).(color.RGBA)
}

// premultiplied 把非预乘的 (r,g,b,a) 组装为预乘的 color.RGBA，供 canvas 反预乘
// 还原出正确的颜色与不透明度。
//
// a == 0 时真正的预乘值只能是 (0,0,0,0)，会把完全透明色（如透明红 255,0,0,0）
// 的原始 RGB 抹掉，后续渐变色标插值只剩黑色分量、整段渐变丢色。因此 a == 0 时
// 保留原始分量作为 straight 载体：规范预乘值在 a == 0 时必为全 0，非 0 字段只能
// 来自这里；混合端经 RGBA()/NRGBA 模型读取时 a == 0 仍表现为全透明（不可见），
// 只有色标插值按 straight 直读字段，让透明色仍参与 RGB 插值。
func premultiplied(r, g, b, a uint8) color.RGBA {
	if a == 0 {
		return color.RGBA{R: r, G: g, B: b, A: 0}
	}
	if a >= 255 {
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	return color.RGBA{
		R: uint8(uint16(r) * uint16(a) / 255),
		G: uint8(uint16(g) * uint16(a) / 255),
		B: uint8(uint16(b) * uint16(a) / 255),
		A: a,
	}
}
