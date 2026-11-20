package main

import (
	"image"
	"image/color"
	"path/filepath"

	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (m *viewerMenu) clearUnlock() {
	m.unlockOpen = false
	m.unlockFilename, m.unlockError = "", ""
	m.unlockPassword.SetText("")
	m.unlockPassword = widget.Editor{}
	m.unlockUser.SetText("")
	m.unlockPath.SetText("")
}

func (m *viewerMenu) showUnlock(filename string, err error) {
	m.clearUnlock()
	m.open, m.unlockOpen = true, true
	m.unlockFilename = filename
	m.unlockMethod.Value = "password"
	m.unlockPassword.SingleLine, m.unlockUser.SingleLine, m.unlockPath.SingleLine = true, true, true
	m.unlockPassword.Mask = '•'
	m.unlockList.Position = layout.Position{}
	if err != nil && !needsUnlock(err) {
		m.unlockError = err.Error()
	} else if err != nil && m.unlockFilename != "" {
		m.unlockError = "请输入凭据；错误凭据可重试。"
	}
}

func (v *viewer) layoutUnlockDialog(gtx layout.Context, th *material.Theme, size image.Point) {
	m := &v.menu
	width := max(0, min(gtx.Dp(unit.Dp(460)), size.X-gtx.Dp(unit.Dp(24))))
	height := max(0, min(gtx.Dp(unit.Dp(490)), size.Y-gtx.Dp(unit.Dp(24))))
	offset := op.Offset(image.Pt((size.X-width)/2, (size.Y-height)/2)).Push(gtx.Ops)
	defer offset.Pop()
	panel := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	defer panel.Pop()
	event.Op(gtx.Ops, &m.panel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	gtx.Constraints = layout.Exact(image.Pt(width, height))
	m.unlockList.Axis = layout.Vertical
	material.List(th, &m.unlockList).Layout(gtx, 10, func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			switch i {
			case 0:
				return material.H6(th, "解锁加密文档").Layout(gtx)
			case 1:
				return material.Body1(th, filepath.Base(m.unlockFilename)).Layout(gtx)
			case 2:
				return material.RadioButton(th, &m.unlockMethod, "password", "口令").Layout(gtx)
			case 3:
				return material.RadioButton(th, &m.unlockMethod, "file", "口令文件").Layout(gtx)
			case 4:
				return material.RadioButton(th, &m.unlockMethod, "key", "SM2 私钥文件（未加密 PKCS#8）").Layout(gtx)
			case 5:
				if m.unlockMethod.Value == "password" {
					return material.Editor(th, &m.unlockPassword, "输入口令").Layout(gtx)
				}
				return material.Editor(th, &m.unlockPath, "输入凭据文件完整路径").Layout(gtx)
			case 6:
				return material.Editor(th, &m.unlockUser, "接收者（可选）").Layout(gtx)
			case 7:
				return material.Caption(th, "口令文件保留空格和换行；凭据不会保存。").Layout(gtx)
			case 8:
				return material.Caption(th, m.unlockError).Layout(gtx)
			default:
				return layout.Flex{}.Layout(gtx, layout.Rigid(material.Button(th, &m.unlockCancel, "取消").Layout), layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout), layout.Rigid(material.Button(th, &m.unlockApply, "解锁并打开").Layout))
			}
		})
	})
}
