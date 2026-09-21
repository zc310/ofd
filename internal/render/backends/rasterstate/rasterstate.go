// Package rasterstate 提供栅格后端 Push/Pop 时需要镜像保存/恢复的样式
// 状态容器。canvas/gg/其它后端自己的 Push/Pop 只保存画笔与变换矩阵，而
// 填充开关、描边造型、虚线、渐变与填充规则需要随栈一起保存，因此包装成
// 一个独立的纯数据载体，供 internal/render/backends 下的各后端共用。
package rasterstate

import (
	"image/color"

	"github.com/tdewolff/canvas"
)

// SolidPaint 把 color.Color 包装成 canvas.Paint 的纯色画刷，nil 返回空
// Paint（等价于 canvas 的 SetFill(nil)/SetStroke(nil)）。各后端用它把
// stroke 颜色随画笔一起镜像保存，供 CopyStrokeToFill 还原填充。
func SolidPaint(c color.Color) canvas.Paint {
	if c == nil {
		return canvas.Paint{}
	}
	return canvas.Paint{Color: color.RGBAModel.Convert(c).(color.RGBA)}
}

// State 是一次 Push 捕获的样式镜像，Pop 时按字段整体恢复。
type State struct {
	Mx           canvas.Matrix
	FillActive   bool
	StrokeActive bool
	StrokeWidth  float64
	StrokeCap    canvas.Capper
	StrokeJoin   canvas.Joiner
	MiterLimit   float64
	DashOffset   float64
	Dashes       []float64
	FillGradient canvas.Gradient
	StrokeGrad   canvas.Gradient
	StrokePaint  canvas.Paint
	FillRule     canvas.FillRule
}

// Save 把当前样式字段打包为一份 State。dashes 独立拷贝，避免与调用方共享
// 底层数组。
func Save(
	mx canvas.Matrix,
	fillActive, strokeActive bool,
	strokeWidth float64,
	strokeCap canvas.Capper,
	strokeJoin canvas.Joiner,
	miterLimit, dashOffset float64,
	dashes []float64,
	fillGradient, strokeGrad canvas.Gradient,
	strokePaint canvas.Paint,
	fillRule canvas.FillRule,
) State {
	return State{
		Mx:           mx,
		FillActive:   fillActive,
		StrokeActive: strokeActive,
		StrokeWidth:  strokeWidth,
		StrokeCap:    strokeCap,
		StrokeJoin:   strokeJoin,
		MiterLimit:   miterLimit,
		DashOffset:   dashOffset,
		Dashes:       append([]float64(nil), dashes...),
		FillGradient: fillGradient,
		StrokeGrad:   strokeGrad,
		StrokePaint:  strokePaint,
		FillRule:     fillRule,
	}
}
