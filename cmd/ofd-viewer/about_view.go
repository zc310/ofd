package main

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// 内容只布局、记录一次，再按实际高度居中重放，避免固定高度留下空白或重复处理事件。
func (v *viewer) layoutAbout(gtx layout.Context, th *material.Theme, window image.Point) {
	width := min(gtx.Dp(unit.Dp(380)), window.X-gtx.Dp(unit.Dp(32)))
	maxHeight := window.Y - gtx.Dp(unit.Dp(32))
	if width <= 0 || maxHeight <= 0 {
		return
	}
	padding := min(gtx.Dp(unit.Dp(16)), width/4, maxHeight/4)
	contentWidth, availableHeight := width-2*padding, maxHeight-2*padding
	content := gtx
	content.Constraints = layout.Constraints{Min: image.Pt(contentWidth, 0), Max: image.Pt(contentWidth, availableHeight)}

	footer := op.Record(gtx.Ops)
	footerDims := layout.Inset{Top: unit.Dp(12)}.Layout(content, func(gtx layout.Context) layout.Dimensions {
		return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			button := material.ButtonLayout(th, &v.menu.closeAbout)
			button.Background = color.NRGBA{R: 229, G: 235, B: 243, A: 255}
			button.CornerRadius = unit.Dp(6)
			return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				semantic.DescriptionOp("关闭关于").Add(gtx.Ops)
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx,
					material.Body2(th, "关闭").Layout)
			})
		})
	})
	footerCall := footer.Stop()

	content.Constraints.Max.Y = max(0, availableHeight-footerDims.Size.Y)
	body := op.Record(gtx.Ops)
	bodyDims := v.layoutAboutContent(content, th)
	bodyCall := body.Stop()
	size := image.Pt(width, bodyDims.Size.Y+footerDims.Size.Y+2*padding)
	defer op.Offset(image.Pt((window.X-size.X)/2, (window.Y-size.Y)/2)).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(size)
	paintLinkPreviewShadow(gtx)
	defer clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &v.menu.panel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 183, G: 192, B: 204, A: 255})
	border := gtx.Dp(unit.Dp(1))
	inner := image.Rectangle{Max: size}.Inset(border)
	paint.FillShape(gtx.Ops, color.NRGBA{R: 250, G: 251, B: 253, A: 255},
		clip.UniformRRect(inner, max(0, gtx.Dp(unit.Dp(8))-border)).Op(gtx.Ops))

	offset := op.Offset(image.Pt(padding, padding)).Push(gtx.Ops)
	bodyCall.Add(gtx.Ops)
	offset.Pop()
	offset = op.Offset(image.Pt(padding, padding+bodyDims.Size.Y)).Push(gtx.Ops)
	footerCall.Add(gtx.Ops)
	offset.Pop()
	semantic.DescriptionOp("关于 OFD Viewer").Add(gtx.Ops)
}

func (v *viewer) layoutAboutContent(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if gtx.Constraints.Max.Y <= 0 {
		return layout.Dimensions{}
	}
	if v.menu.aboutIcon == nil {
		icon := loadViewerIcon()
		v.menu.aboutIcon = &icon
	}
	lines := aboutLines()
	v.menu.aboutList.Axis = layout.Vertical
	return material.List(th, &v.menu.aboutList).Layout(gtx, len(lines)+1, func(gtx layout.Context, index int) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if index == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := min(gtx.Dp(unit.Dp(80)), gtx.Constraints.Max.X)
					gtx.Constraints = layout.Exact(image.Pt(size, size))
					return (widget.Image{Src: *v.menu.aboutIcon, Fit: widget.Contain}).Layout(gtx)
				})
			}
			index--
			if index == 0 {
				label := material.H6(th, lines[index])
				label.Alignment = text.Middle
				return label.Layout(gtx)
			}
			if index == len(lines)-1 {
				return material.Clickable(gtx, &v.menu.aboutLink, func(gtx layout.Context) layout.Dimensions {
					semantic.DescriptionOp(viewerProjectURL).Add(gtx.Ops)
					label := material.Body2(th, lines[index])
					label.Color, label.Alignment = th.ContrastBg, text.Middle
					return label.Layout(gtx)
				})
			}
			label := material.Body2(th, lines[index])
			label.Alignment = text.Middle
			if index != 2 {
				label = material.Caption(th, lines[index])
				label.Alignment = text.Middle
				label.Color = color.NRGBA{R: 111, G: 122, B: 138, A: 255}
			}
			return label.Layout(gtx)
		})
	})
}
