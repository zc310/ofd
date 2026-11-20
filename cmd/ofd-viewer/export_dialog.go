package main

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (m *viewerMenu) exportOptions() (exportOptions, error) {
	format := m.exportFormat.Value
	valid := false
	for _, f := range exportFormats {
		if format == f {
			valid = true
			break
		}
	}
	if !valid {
		return exportOptions{}, viewerError("请选择导出格式")
	}
	dpi := exportDPI
	if format == "png" || format == "jpg" {
		var err error
		dpi, err = strconv.Atoi(strings.TrimSpace(m.exportDPIInput.Text()))
		if err != nil || dpi < 1 || dpi > 1200 {
			return exportOptions{}, viewerError("DPI 必须在 1 到 1200 之间")
		}
	}
	return exportOptions{format: format, dpi: dpi, white: m.exportBackground.Value == "white"}, nil
}

// 弹层各行独立虚拟化，较矮窗口也可滚动到确认按钮。
func (v *viewer) layoutExportDialog(gtx layout.Context, th *material.Theme, size image.Point) {
	m := &v.menu
	width := max(0, min(gtx.Dp(unit.Dp(420)), size.X-gtx.Dp(unit.Dp(24))))
	height := max(0, min(gtx.Dp(unit.Dp(560)), size.Y-gtx.Dp(unit.Dp(24))))
	offset := op.Offset(image.Pt((size.X-width)/2, (size.Y-height)/2)).Push(gtx.Ops)
	defer offset.Pop()
	panel := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	defer panel.Pop()
	event.Op(gtx.Ops, &m.panel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	gtx.Constraints = layout.Exact(image.Pt(width, height))
	m.exportList.Axis = layout.Vertical
	material.List(th, &m.exportList).Layout(gtx, 12, func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			switch {
			case i == 0:
				return material.H6(th, "导出文档").Layout(gtx)
			case i == 1:
				return material.Body1(th, "格式（多页图片打包为 ZIP）").Layout(gtx)
			case i >= 2 && i < 7:
				f := exportFormats[i-2]
				return material.RadioButton(th, &m.exportFormat, f, strings.ToUpper(f)).Layout(gtx)
			case i == 7:
				if m.exportFormat.Value != "png" && m.exportFormat.Value != "jpg" {
					return layout.Dimensions{}
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.Body1(th, "DPI (1–1200)  ").Layout),
					layout.Flexed(1, material.Editor(th, &m.exportDPIInput, "150").Layout))
			case i == 8:
				if m.exportFormat.Value == "txt" {
					return layout.Dimensions{}
				}
				return material.Body1(th, "纸张背景（JPEG 不支持透明）").Layout(gtx)
			case i == 9:
				if m.exportFormat.Value == "txt" {
					return layout.Dimensions{}
				}
				return layout.Flex{}.Layout(gtx, layout.Flexed(1, material.RadioButton(th, &m.exportBackground, "transparent", "透明").Layout), layout.Flexed(1, material.RadioButton(th, &m.exportBackground, "white", "白色").Layout))
			case i == 10:
				return material.Caption(th, m.exportError).Layout(gtx)
			default:
				return layout.Flex{}.Layout(gtx, layout.Rigid(material.Button(th, &m.exportCancel, "取消").Layout), layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout), layout.Rigid(material.Button(th, &m.exportApply, "选择保存位置").Layout))
			}
		})
	})
}
