package main

import (
	"image"
	"math"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const (
	defaultWindowWidth = unit.Dp(700)
	readingMargin      = unit.Dp(12)
)

// 初始窗口扣除工具栏、状态栏和阅读边距后，按 A4 竖版比例计算。
// 系统可以限制窗口尺寸；用户调整窗口后不再自动改回该尺寸。
func defaultWindowSize(th *material.Theme) (unit.Dp, unit.Dp) {
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(int(defaultWindowWidth), 2000)},
	}
	var v viewer
	header := v.layoutHeader(gtx, th).Size.Y
	ops.Reset()
	status := v.layoutStatus(gtx, th).Size.Y
	pageWidth := float64(defaultWindowWidth - 2*readingMargin)
	pageHeight := math.Ceil(pageWidth * 297 / 210)
	return defaultWindowWidth, unit.Dp(pageHeight) + 2*readingMargin + unit.Dp(header+status)
}

func (v *viewer) layoutStatus(gtx layout.Context, th *material.Theme) layout.Dimensions {
	status := v.status
	if status == "" {
		if v.compactToolbar {
			return layout.Dimensions{}
		}
		if v.continuous.enabled {
			status = "Ctrl+滚轮：缩放 | 拖动 / 滚轮：连续滚动 | 方向键：翻页 | G：跳转 | Esc：退出"
		} else {
			status = "Ctrl+滚轮：缩放 | 拖动 / 滚轮：平移 | 方向键：翻页 | G：跳转 | Esc：退出"
		}
	}
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, material.Caption(th, status).Layout)
}
