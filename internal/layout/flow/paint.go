package flow

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
)

// 默认配色。纯黑文字在屏幕上偏硬，正文使用近黑色。
var (
	colorText         = &creator.Color{R: 0x1f, G: 0x23, B: 0x28}
	colorLink         = &creator.Color{R: 0x0b, G: 0x57, B: 0xd0}
	colorCode         = &creator.Color{R: 0xa6, G: 0x1e, B: 0x4e}
	colorQuote        = &creator.Color{R: 0x57, G: 0x60, B: 0x6a}
	colorCodeBG       = &creator.Color{R: 0xf4, G: 0xf5, B: 0xf7}
	colorCodeLabel    = &creator.Color{R: 0x6b, G: 0x74, B: 0x80}
	colorInlineCodeBG = &creator.Color{R: 0xe9, G: 0xec, B: 0xf0}
	colorRule         = &creator.Color{R: 0xd0, G: 0xd7, B: 0xde}
	colorBorder       = &creator.Color{R: 0xc4, G: 0xcb, B: 0xd1}
	colorHeaderBG     = &creator.Color{R: 0xef, G: 0xf2, B: 0xf5}
)

// linkActions 把点击目标转成图元的 CLICK 动作；空目标只保留文字样式。
func linkActions(href string) []creator.Action {
	if href == "" {
		return nil
	}
	return []creator.Action{{
		Event: creator.ActionEventClick,
		URI:   &creator.URIAction{URI: href},
	}}
}

// fillRect 的 y 是自底向上的下边界；creator 路径坐标从页顶向下，因此在此翻转。
func (e *engine) fillRect(x, y, width, height float64, color *creator.Color) {
	if width <= 0 || height <= 0 {
		return
	}
	stroke := false
	e.add(creator.Path{
		X:         x,
		Y:         e.geometry.PageHeight - (y + height),
		Width:     width,
		Height:    height,
		Data:      rectPath(width, height),
		Fill:      true,
		Stroke:    stroke,
		StrokeSet: &stroke,
		LineWidth: 0,
		FillColor: color,
	})
}

func rectPath(width, height float64) string {
	return fmt.Sprintf("M 0 0 L %g 0 L %g %g L 0 %g C", width, width, height, height)
}

// fillRoundedPanel 与 fillRect 使用相同坐标；圆角半径不能超过短边的一半。
func (e *engine) fillRoundedPanel(x, y, width, height, radius float64, color *creator.Color) {
	if width <= 0 || height <= 0 {
		return
	}
	radius = math.Min(radius, math.Min(width, height)/2)
	if radius <= 0 {
		e.fillRect(x, y, width, height, color)
		return
	}
	stroke := false
	e.add(creator.Path{
		X:         x,
		Y:         e.geometry.PageHeight - (y + height),
		Width:     width,
		Height:    height,
		Data:      roundedRectPath(width, height, radius),
		Fill:      true,
		Stroke:    stroke,
		StrokeSet: &stroke,
		LineWidth: 0,
		FillColor: color,
	})
}

// roundedRectPath 以四段直线和四分之一圆弧构造顺时针闭合路径。
func roundedRectPath(width, height, r float64) string {
	return fmt.Sprintf(
		"M %g %g L %g %g A %g %g 0 0 1 %g %g L %g %g A %g %g 0 0 1 %g %g L %g %g A %g %g 0 0 1 %g %g L %g %g A %g %g 0 0 1 %g %g C",
		r, 0.0,
		width-r, 0.0,
		r, r, width, r,
		width, height-r,
		r, r, width-r, height,
		r, height,
		r, r, 0.0, height-r,
		0.0, r,
		r, r, r, 0.0,
	)
}
