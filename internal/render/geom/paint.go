package geom

import "image/color"

// Paint 是填充/描边画笔：纯色或渐变，二者择一。
type Paint struct {
	Color    color.Color
	Gradient Gradient
}

// SolidPaint 返回纯色画笔。
func SolidPaint(c color.Color) Paint { return Paint{Color: c} }

// GradientPaint 返回渐变画笔。
func GradientPaint(g Gradient) Paint { return Paint{Gradient: g} }

// IsColor 判断画笔是否为纯色。
func (p Paint) IsColor() bool { return p.Color != nil && p.Gradient == nil }

// IsGradient 判断画笔是否为渐变。
func (p Paint) IsGradient() bool { return p.Gradient != nil }

// Has 判断画笔是否可绘制。
func (p Paint) Has() bool { return p.Color != nil || p.Gradient != nil }
