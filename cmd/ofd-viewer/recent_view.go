package main

import (
	"image"
	"image/color"
	"path/filepath"
	"strconv"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func recentFilesFrame(window image.Point, toolbarBottom, count int, scale float32) image.Rectangle {
	margin := max(1, int(12*scale))
	top := max(margin, toolbarBottom)
	width := min(int(560*scale), window.X-2*margin)
	height := min(int(float32(52+68*max(1, count)+44)*scale), window.Y-top-margin)
	if width <= 0 || height <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(margin, top, margin+width, top+height)
}

// 最近文档叠放在阅读区上方，不改变页面视口；卡片外点击只关闭列表，不传给正文。
func (v *viewer) layoutRecentOverlay(gtx layout.Context, th *material.Theme) {
	if !v.showRecent {
		return
	}
	frame := recentFilesFrame(gtx.Constraints.Max, v.recentTop, len(v.recentFiles), max(1, gtx.Metric.PxPerDp))
	if frame.Empty() {
		return
	}
	backdrop := gtx
	backdrop.Constraints = layout.Exact(gtx.Constraints.Max)
	// 工具栏保持可操作，再次点击最近文档图标即可收起。
	area := clip.Rect{Min: image.Pt(0, v.recentTop), Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.recentOutside)
	v.recentBackdrop.Layout(backdrop, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})
	area.Pop()
	defer op.Offset(frame.Min).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(frame.Size())
	paintLinkPreviewShadow(gtx)
	defer clip.UniformRRect(image.Rectangle{Max: frame.Size()}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &v.recentPanel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 183, G: 192, B: 204, A: 255})
	layout.UniformInset(unit.Dp(1)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(7))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, color.NRGBA{R: 250, G: 251, B: 253, A: 255})
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutRecentHeader(gtx, th)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if len(v.recentFiles) == 0 {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(12)).Layout(gtx, material.Body2(th, "暂无最近文档，打开文件后会显示在这里").Layout)
					})
				}
				if v.busy {
					gtx = gtx.Disabled()
				}
				v.recentList.Axis = layout.Vertical
				return material.List(th, &v.recentList).Layout(gtx, len(v.recentFiles), func(gtx layout.Context, index int) layout.Dimensions {
					return v.layoutRecentEntry(gtx, th, index)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if v.busy || len(v.recentFiles) == 0 {
						gtx = gtx.Disabled()
					}
					button := material.Button(th, &v.clearRecent, "清空记录")
					button.Background = color.NRGBA{}
					button.Color = color.NRGBA{R: 105, G: 115, B: 130, A: 255}
					button.TextSize = unit.Sp(12)
					button.Inset = layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}
					return layout.E.Layout(gtx, button.Layout)
				})
			}))
	})
}

func (v *viewer) layoutRecentHeader(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				title := material.Body1(th, "最近文档")
				title.Font.Weight = font.SemiBold
				title.MaxLines = 1
				return title.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Caption(th, strconv.Itoa(len(v.recentFiles))+" 项")
				label.Color = color.NRGBA{R: 115, G: 125, B: 140, A: 255}
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Clickable(gtx, &v.closeRecent, func(gtx layout.Context) layout.Dimensions {
					semantic.DescriptionOp("关闭最近文档").Add(gtx.Ops)
					return layout.UniformInset(unit.Dp(10)).Layout(gtx, material.Body1(th, "×").Layout)
				})
			}))
	})
}

func (v *viewer) layoutRecentEntry(gtx layout.Context, th *material.Theme, index int) layout.Dimensions {
	path := v.recentFiles[index]
	button := &v.recentEntries[index]
	current := v.isCurrentRecentFile(path)
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		background := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		if current {
			background = color.NRGBA{R: 232, G: 241, B: 252, A: 255}
		}
		if button.Hovered() {
			background = color.NRGBA{R: 220, G: 233, B: 249, A: 255}
		}
		style := material.ButtonLayout(th, button)
		style.Background, style.CornerRadius = background, unit.Dp(6)
		return style.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.DescriptionOp(path).Add(gtx.Ops)
			semantic.SelectedOp(current).Add(gtx.Ops)
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(9), Bottom: unit.Dp(9)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							size := min(gtx.Dp(unit.Dp(30)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
							gtx.Constraints = layout.Exact(image.Pt(size, size))
							v.toolbarIcons.init()
							if v.toolbarIcons.document == nil {
								return layout.Dimensions{Size: gtx.Constraints.Min}
							}
							return v.toolbarIcons.document.Layout(gtx, color.NRGBA{R: 83, G: 120, B: 162, A: 255})
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								title := material.Body2(th, filepath.Base(path))
								title.Font.Weight, title.MaxLines = font.SemiBold, 1
								return title.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								directory := material.Caption(th, filepath.Dir(path))
								directory.Color = color.NRGBA{R: 111, G: 122, B: 138, A: 255}
								directory.MaxLines = 1
								return directory.Layout(gtx)
							}))
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !current {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							label := material.Caption(th, "当前")
							label.Color = color.NRGBA{R: 61, G: 106, B: 161, A: 255}
							return label.Layout(gtx)
						})
					}))
			})
		})
	})
}

func (v *viewer) isCurrentRecentFile(path string) bool {
	if v.total <= 0 || v.thumbnails.filename == "" {
		return false
	}
	current, err := filepath.Abs(v.thumbnails.filename)
	return err == nil && filepath.Clean(path) == current
}
