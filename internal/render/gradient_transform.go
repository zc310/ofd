package render

import (
	"image/color"

	"github.com/tdewolff/canvas"
)

// translatedGradient 将页面坐标系中的渐变适配到局部栅格画布，
// 同时保持渐变的视觉起点不变。
type translatedGradient struct {
	canvas.Gradient
	dx, dy float64
}

func translateGradient(gradient canvas.Gradient, dx, dy float64) canvas.Gradient {
	if gradient == nil || (dx == 0 && dy == 0) {
		return gradient
	}
	return &translatedGradient{Gradient: gradient, dx: dx, dy: dy}
}

func (g *translatedGradient) SetColorSpace(colorSpace canvas.ColorSpace) canvas.Gradient {
	g.Gradient = g.Gradient.SetColorSpace(colorSpace)
	return g
}

func (g *translatedGradient) At(x, y float64) color.RGBA {
	return g.Gradient.At(x+g.dx, y+g.dy)
}
