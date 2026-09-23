// Package rasterstate 提供栅格后端 Push/Pop 时需要镜像保存/恢复的样式
// 状态容器。各后端自己的 Push/Pop 只保存画笔与变换矩阵，而填充开关、描边
// 造型、虚线、渐变与填充规则需要随栈一起保存，因此包装成一个独立的纯数据
// 载体，供 internal/render/backends 下的各后端共用。
//
// 状态只使用与绘制库无关的 geom 类型，后端无需依赖 tdewolff/canvas。
package rasterstate

import (
	"image/color"

	"github.com/zc310/ofd/internal/render/geom"
)

// SolidPaint 把 color.Color 包装成 geom.Paint 的纯色画刷，nil 返回空
// Paint（等价于清除画笔）。各后端用它把 stroke 颜色随画笔一起镜像保存，
// 供 CopyStrokeToFill 还原填充。
func SolidPaint(c color.Color) geom.Paint {
	if c == nil {
		return geom.Paint{}
	}
	return geom.SolidPaint(c)
}

// State 是一次 Push 捕获的样式镜像，Pop 时按字段整体恢复。
type State struct {
	Mx           geom.Matrix
	FillActive   bool
	StrokeActive bool
	StrokeWidth  float64
	StrokeCap    geom.Capper
	StrokeJoin   geom.Joiner
	MiterLimit   float64
	DashOffset   float64
	Dashes       []float64
	FillGradient geom.Gradient
	StrokeGrad   geom.Gradient
	StrokePaint  geom.Paint
	FillRule     geom.FillRule
}

// Save 把当前样式字段打包为一份 State。dashes 独立拷贝，避免与调用方共享
// 底层数组。
func Save(
	mx geom.Matrix,
	fillActive, strokeActive bool,
	strokeWidth float64,
	strokeCap geom.Capper,
	strokeJoin geom.Joiner,
	miterLimit, dashOffset float64,
	dashes []float64,
	fillGradient, strokeGrad geom.Gradient,
	strokePaint geom.Paint,
	fillRule geom.FillRule,
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
