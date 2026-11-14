package main

import (
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"

	"gioui.org/f32"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

// widget.Icon 内部缓存光栅化结果，因此每个窗口各自持有，不在窗口间并发共享。
type toolbarIcons struct {
	initialized                  bool
	open, recent, previous, next *widget.Icon
	zoomOut, zoomIn              *widget.Icon
	menu                         *widget.Icon
	thumbnail, fitPage, fitWidth *widget.Icon
	continuous, pageControls     *widget.Icon
	about, exit                  *widget.Icon
	background                   *widget.Icon
}

func (i *toolbarIcons) init() {
	if i.initialized {
		return
	}
	i.initialized = true
	load := func(name string, data []byte) *widget.Icon {
		icon, err := widget.NewIcon(data)
		if err != nil {
			slog.Warn("工具栏图标不可用，使用文字", "icon", name, "error", err)
		}
		return icon
	}
	i.open = load("打开", icons.FileFolderOpen)
	i.recent = load("最近文件", icons.ActionHistory)
	i.previous = load("上一页", icons.NavigationChevronLeft)
	i.next = load("下一页", icons.NavigationChevronRight)
	i.zoomOut = load("缩小", icons.ActionZoomOut)
	i.zoomIn = load("放大", icons.ActionZoomIn)
	i.menu = load("菜单", icons.NavigationMoreVert)
	i.thumbnail = load("缩略图", icons.ActionViewModule)
	i.fitPage = load("适合页面", icons.ImageCropFree)
	i.fitWidth = load("适合宽度", icons.ImageCropLandscape)
	i.continuous = load("连续阅读", icons.ActionViewStream)
	i.pageControls = load("底部页码", icons.ActionChromeReaderMode)
	i.about = load("关于", icons.ActionInfoOutline)
	i.exit = load("退出", icons.ActionExitToApp)
	i.background = load("背景色", icons.ImagePalette)
}

// 菜单固定在工具栏右侧，不随工具栏换行或滚动离开窗口。
func (v *viewer) layoutHeader(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.compactToolbar = gtx.Constraints.Max.X < gtx.Dp(unit.Dp(600))
	return layout.Flex{Alignment: layout.Start}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return v.layoutToolbar(gtx, th) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				v.toolbarIcons.init()
				return toolbarSquareButton(th, &v.menu.button, v.toolbarIcons.menu, "菜单", false, v.menu.open)(gtx)
			})
		}))
}

// 顶栏使用中性圆角矩形，与 Fyne 版工具栏一致，并保留适合触摸的点击面积。
func toolbarSquareButton(th *material.Theme, button *widget.Clickable, icon *widget.Icon, label string, disabled, active bool) layout.Widget {
	if icon == nil {
		return toolbarButton(th, button, label, disabled, active)
	}
	return func(gtx layout.Context) layout.Dimensions {
		if disabled {
			gtx = gtx.Disabled()
		}
		size := min(gtx.Dp(unit.Dp(54)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
		gtx.Constraints = layout.Exact(image.Pt(size, size))
		return layout.UniformInset(unit.Dp(3)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			style := material.ButtonLayout(th, button)
			style.CornerRadius = unit.Dp(5)
			style.Background = color.NRGBA{R: 229, G: 231, B: 234, A: 255}
			if active {
				style.Background = color.NRGBA{R: 185, G: 208, B: 232, A: 255}
			}
			return style.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				semantic.DescriptionOp(label).Add(gtx.Ops)
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					iconSize := min(gtx.Dp(unit.Dp(22)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
					gtx.Constraints = layout.Exact(image.Pt(iconSize, iconSize))
					foreground := color.NRGBA{R: 48, G: 52, B: 58, A: 255}
					if disabled {
						foreground = color.NRGBA{R: 155, G: 158, B: 163, A: 255}
					}
					return icon.Layout(gtx, foreground)
				})
			})
		})
	}
}

func (v *viewer) handleScaleButtons(gtx layout.Context, settings *viewerSettings, persist func()) {
	if v.fitPageButton.Clicked(gtx) && !v.busy && v.scene != nil {
		v.menu.open = false
		v.viewport.fit(fitWhole)
		settings.Fit = "page"
		persist()
	}
	if v.fitWidthButton.Clicked(gtx) && !v.busy && v.scene != nil {
		v.menu.open = false
		v.viewport.fit(fitWidth)
		settings.Fit = "width"
		persist()
	}
	center := f32.Pt(float32(v.viewport.viewport.X)/2, float32(v.viewport.viewport.Y)/2)
	if v.zoomIn.Clicked(gtx) && !v.busy && v.scene != nil {
		v.menu.open = false
		v.zoomViewport(1.2, center, gtx.Metric.PxPerDp)
	}
	if v.zoomOut.Clicked(gtx) && !v.busy && v.scene != nil {
		v.menu.open = false
		v.zoomViewport(1/1.2, center, gtx.Metric.PxPerDp)
	}
}

// 页码控件叠放在阅读区底部，不改变页面视口尺寸。
func (v *viewer) layoutPageControls(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.toolbarIcons.init()
	return layout.Inset{Bottom: unit.Dp(12), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{R: 242, G: 243, B: 245, A: 248}, clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(unit.Dp(14))).Op(gtx.Ops))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(pageNavigationButton(th, &v.previous, v.toolbarIcons.previous, "上一页", v.busy || v.total == 0 || v.page <= 0)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.busy || v.total == 0 {
							gtx = gtx.Disabled()
						}
						width := min(gtx.Dp(unit.Dp(55)), gtx.Constraints.Max.X)
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = width, width
						borderColor := color.NRGBA{R: 185, G: 190, B: 197, A: 255}
						if gtx.Focused(&v.pageInput) {
							borderColor = color.NRGBA{R: 67, G: 117, B: 173, A: 255}
						}
						border := widget.Border{Color: borderColor, CornerRadius: unit.Dp(6), Width: unit.Dp(1)}
						return border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, material.Editor(th, &v.pageInput, "页码").Layout)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(5), Right: unit.Dp(5)}.Layout(gtx, material.Body1(th, fmt.Sprintf("/ %d", v.total)).Layout)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.busy || v.total == 0 {
							gtx = gtx.Disabled()
						}
						button := material.Button(th, &v.jumpButton, "跳转")
						button.Background = color.NRGBA{R: 221, G: 225, B: 230, A: 255}
						button.Color = color.NRGBA{R: 48, G: 52, B: 58, A: 255}
						button.CornerRadius = unit.Dp(6)
						return button.Layout(gtx)
					}),
					layout.Rigid(pageNavigationButton(th, &v.next, v.toolbarIcons.next, "下一页", v.busy || v.total == 0 || v.page+1 >= v.total)),
				)
			}))
	})
}

// 翻页图标使用透明背景，避免在浮动栏上出现与工具栏不一致的实心圆。
func pageNavigationButton(th *material.Theme, button *widget.Clickable, icon *widget.Icon, label string, disabled bool) layout.Widget {
	if icon == nil {
		return toolbarButton(th, button, label, disabled, false)
	}
	return func(gtx layout.Context) layout.Dimensions {
		if disabled {
			gtx = gtx.Disabled()
		}
		style := material.IconButton(th, button, icon, label)
		style.Background = color.NRGBA{}
		style.Color = color.NRGBA{R: 48, G: 52, B: 58, A: 255}
		if disabled {
			style.Color = color.NRGBA{R: 160, G: 164, B: 170, A: 255}
		}
		return style.Layout(gtx)
	}
}

// toolbarFlow 先记录每个控件，再按实际尺寸换行；控件只布局一次，事件区域随操作一起平移。
func toolbarFlow(gtx layout.Context, gap int, items ...layout.Widget) layout.Dimensions {
	width := gtx.Constraints.Max.X
	var x, y, rowHeight int
	clipStack := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	defer clipStack.Pop()
	for _, item := range items {
		child := gtx
		child.Constraints = layout.Constraints{Max: image.Pt(width, gtx.Constraints.Max.Y)}
		macro := op.Record(gtx.Ops)
		dims := item(child)
		call := macro.Stop()
		if x > 0 && x+gap+dims.Size.X > width {
			x, y, rowHeight = 0, y+rowHeight+gap, 0
		}
		if x > 0 {
			x += gap
		}
		offset := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		offset.Pop()
		x += dims.Size.X
		rowHeight = max(rowHeight, dims.Size.Y)
	}
	return layout.Dimensions{Size: gtx.Constraints.Constrain(image.Pt(width, y+rowHeight))}
}

func toolbarButton(th *material.Theme, button *widget.Clickable, label string, disabled, active bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if disabled {
			gtx = gtx.Disabled()
		}
		style := material.Button(th, button, label)
		if active {
			style.Background = color.NRGBA{R: 30, G: 92, B: 155, A: 255}
		}
		return style.Layout(gtx)
	}
}

// toolbarIconButton 保留可见文字与语义描述；图标不可用时退回文字按钮。
func toolbarIconButton(th *material.Theme, button *widget.Clickable, icon *widget.Icon, label string, disabled, active bool) layout.Widget {
	if icon == nil {
		return toolbarButton(th, button, label, disabled, active)
	}
	return func(gtx layout.Context) layout.Dimensions {
		if disabled {
			gtx = gtx.Disabled()
		}
		style := material.ButtonLayout(th, button)
		if active {
			style.Background = color.NRGBA{R: 30, G: 92, B: 155, A: 255}
		}
		return style.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			semantic.DescriptionOp(label).Add(gtx.Ops)
			return (layout.Inset{Top: 10, Bottom: 10, Left: 12, Right: 12}).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						size := min(gtx.Dp(unit.Dp(18)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
						if size <= 0 {
							return layout.Dimensions{}
						}
						gtx.Constraints.Min = image.Pt(size, size)
						return icon.Layout(gtx, th.ContrastFg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						labelStyle := material.Body2(th, label)
						labelStyle.Color = th.ContrastFg
						return labelStyle.Layout(gtx)
					}))
			})
		})
	}
}

func (v *viewer) layoutToolbar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// 极矮窗口中保留阅读区；工具栏自身可滚动，不丢失换行后的控件。
	gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, max(gtx.Dp(unit.Dp(80)), gtx.Constraints.Max.Y/2))
	gtx.Constraints.Min.Y = min(gtx.Constraints.Min.Y, gtx.Constraints.Max.Y)
	v.toolbarList.Axis = layout.Vertical
	return material.List(th, &v.toolbarList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return v.layoutToolbarContent(gtx, th)
	})
}

func (v *viewer) layoutToolbarContent(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		v.toolbarIcons.init()
		disabled := v.busy || v.scene == nil
		thumbnails := "缩略图：关"
		if v.thumbnails.open {
			thumbnails = "缩略图：开"
		}
		zoom := v.zoomLabel(gtx.Metric.PxPerDp)
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(toolbarSquareButton(th, &v.open, v.toolbarIcons.open, "打开 (O)", v.busy, false)),
			layout.Rigid(toolbarSquareButton(th, &v.recentButton, v.toolbarIcons.recent, "最近文件", v.busy, v.showRecent)),
			layout.Rigid(toolbarSquareButton(th, &v.thumbnailButton, v.toolbarIcons.thumbnail, thumbnails, disabled, v.thumbnails.open)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.compactToolbar {
					return layout.Dimensions{}
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(toolbarSquareButton(th, &v.zoomOut, v.toolbarIcons.zoomOut, "缩小", disabled, false)),
					layout.Rigid(zoomIndicator(th, zoom)),
					layout.Rigid(toolbarSquareButton(th, &v.zoomIn, v.toolbarIcons.zoomIn, "放大", disabled, false)))
			}),
		)
	})
}

// 后台加载页面时 scene 会暂时置空，仍显示当前文档的缩放比例。
func (v *viewer) zoomLabel(pixelsPerDp float32) string {
	if v.total <= 0 || v.viewport.scale <= 0 {
		return "—"
	}
	percent := v.viewport.scale / (96.0 / 25.4 * max(pixelsPerDp, 1)) * 100
	return fmt.Sprintf("%.0f%%", math.Round(float64(percent)))
}

// 固定比例文本宽度，避免跨页或缩放时数字位数变化推动相邻按钮。
func zoomIndicator(th *material.Theme, value string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		width := min(gtx.Dp(unit.Dp(72)), gtx.Constraints.Max.X)
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = width, width
		label := material.Body1(th, value)
		label.MaxLines = 1
		return layout.Center.Layout(gtx, label.Layout)
	}
}
