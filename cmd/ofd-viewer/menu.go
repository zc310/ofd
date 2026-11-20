package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	_ "image/png"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/colorpicker"
	"github.com/zc310/ofd/internal/version"
)

const (
	viewerProjectURL    = "https://github.com/zc310/ofd"
	viewerApplicationID = "io.github.zc310.ofd"
)

//go:embed Icon.png
var viewerIconData []byte

func loadViewerIcon() paint.ImageOp {
	icon, _, err := image.Decode(bytes.NewReader(viewerIconData))
	if err != nil {
		panic("读取内嵌图标失败: " + err.Error())
	}
	return paint.NewImageOp(icon)
}

type viewerMenu struct {
	button, backdrop, aboutItem, exitItem, closeAbout widget.Clickable
	aboutLink                                         widget.Clickable
	defaultReaderItem                                 widget.Clickable
	backgroundItem, backgroundBack, backgroundApply   widget.Clickable
	exportItem, exportCancel, exportApply             widget.Clickable
	exportFormat, exportBackground                    widget.Enum
	exportDPIInput                                    widget.Editor
	exportList                                        widget.List
	exportError                                       string
	unlockOpen                                        bool
	unlockFilename, unlockError                       string
	unlockMethod                                      widget.Enum
	unlockPassword, unlockUser, unlockPath            widget.Editor
	unlockCancel, unlockApply                         widget.Clickable
	unlockList                                        widget.List
	exportOpen                                        bool
	backgroundCustomItem, backgroundCancel            widget.Clickable
	backgroundButtons                                 [len(backgroundPresets)]widget.Clickable
	backgroundPicker                                  colorpicker.State
	backgroundPickerList                              widget.List
	backgroundPicking                                 bool
	backgroundList                                    widget.List
	backgroundOpen                                    bool
	backgroundError                                   string
	open, about                                       bool
	aboutList                                         widget.List
	aboutIcon                                         *paint.ImageOp
	list                                              widget.List
	panel                                             struct{}
}

func (v *viewer) handleViewMenu(gtx layout.Context, settings *viewerSettings, persist func(), submit func(request)) {
	if v.menu.open && v.menu.backgroundOpen {
		for i := range backgroundPresets {
			if v.menu.backgroundButtons[i].Clicked(gtx) {
				v.background = backgroundPresets[i].key
				settings.Background = v.background
				v.menu.backgroundOpen, v.menu.open = false, false
				persist()
			}
		}
		if v.menu.backgroundPicking {
			v.menu.backgroundPicker.Update(gtx)
		}
		if v.menu.backgroundPicking && v.menu.backgroundApply.Clicked(gtx) {
			if _, ok := parseBackgroundColor("#" + v.menu.backgroundPicker.Text()); !ok || len(v.menu.backgroundPicker.Text()) != 6 {
				v.menu.backgroundError = "请输入 6 位 RGB 十六进制颜色"
			} else {
				c := v.menu.backgroundPicker.Color()
				value := fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
				v.background, v.backgroundCustom = "custom", value
				settings.Background, settings.BackgroundCustom = v.background, value
				v.menu.backgroundError = ""
				v.menu.backgroundPicking, v.menu.backgroundOpen, v.menu.open = false, false, false
				gtx.Execute(key.FocusCmd{})
				persist()
			}
		}
	}
	if v.continuousButton.Clicked(gtx) && !v.busy && v.scene != nil {
		v.menu.open = false
		v.toggleContinuous(submit)
		settings.Continuous = v.continuous.enabled
		if !v.continuous.enabled {
			v.continuous.double = false
		}
		settings.DoublePage = v.continuous.double
		persist()
	}
	if v.doublePageButton.Clicked(gtx) && !v.busy && v.scene != nil {
		v.menu.open = false
		v.continuous.double = !v.continuous.double
		if !v.continuous.enabled {
			v.continuous.enabled = true
		}
		if v.continuous.entries == nil {
			v.continuous.reset(v.page)
		}
		v.continuous.jump(v.page)
		v.continuous.first = -1
		clear(v.continuous.blocked)
		settings.Continuous, settings.DoublePage = v.continuous.enabled, v.continuous.double
		persist()
	}
	if v.pageControlsButton.Clicked(gtx) {
		v.hidePageControls = !v.hidePageControls
		settings.HidePageControls = v.hidePageControls
		v.menu.open = false
		persist()
	}
}

// handleMenu 优先消费 Esc，防止关闭弹层时同时退出阅读器。
func (v *viewer) handleMenu(gtx layout.Context, closeWindow func()) {
	m := &v.menu
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &m.panel, Kinds: pointer.Press | pointer.Release | pointer.Cancel}); !ok {
			break
		}
	}
	if m.button.Clicked(gtx) && !m.about {
		m.open = !m.open
		if m.open {
			m.list.Position = layout.Position{}
		}
		m.backgroundOpen = false
		m.backgroundPicking = false
		m.exportOpen = false
		gtx.Execute(key.FocusCmd{})
	}
	if m.backdrop.Clicked(gtx) {
		m.open, m.about = false, false
		m.backgroundOpen = false
		m.backgroundPicking = false
		m.exportOpen = false
		m.clearUnlock()
	}
	if m.open && !m.backgroundOpen && m.backgroundItem.Clicked(gtx) {
		m.backgroundOpen = true
		m.backgroundList.Position = layout.Position{}
		m.backgroundError = ""
	}
	if m.open && !m.backgroundOpen && m.exportItem.Clicked(gtx) {
		m.exportOpen = true
		m.exportList.Position = layout.Position{}
		m.exportFormat.Value = "pdf"
		m.exportBackground.Value = "transparent"
		m.exportDPIInput.SingleLine = true
		m.exportDPIInput.InputHint = key.HintNumeric
		m.exportDPIInput.SetText("150")
		m.exportError = ""
	}
	if m.exportOpen && m.exportCancel.Clicked(gtx) {
		m.exportOpen, m.open = false, false
		gtx.Execute(key.FocusCmd{})
	}
	if m.open && m.backgroundOpen && !m.backgroundPicking && m.backgroundCustomItem.Clicked(gtx) {
		m.backgroundPicking = true
		m.backgroundPicker.SetColor(paperColor(v.background, v.backgroundCustom))
		m.backgroundPicker.SingleLine = true
		m.backgroundPickerList.Position = layout.Position{}
		m.backgroundError = ""
	}
	if m.backgroundPicking && m.backgroundCancel.Clicked(gtx) {
		m.backgroundPicking = false
		gtx.Execute(key.FocusCmd{})
	}
	if m.open && m.backgroundOpen && m.backgroundBack.Clicked(gtx) {
		m.backgroundOpen = false
		gtx.Execute(key.FocusCmd{})
	}
	if m.open && m.aboutItem.Clicked(gtx) {
		m.open, m.about = false, true
		m.aboutList.Position = layout.Position{}
	}
	if m.open && m.defaultReaderItem.Clicked(gtx) && v.defaultReaderAction != nil {
		m.open = false
		v.defaultReaderAction()
	}
	if m.open && m.exitItem.Clicked(gtx) {
		m.open = false
		closeWindow()
	}
	if m.about && m.closeAbout.Clicked(gtx) {
		m.about = false
	}
	if m.about && m.aboutLink.Clicked(gtx) && v.openURL != nil {
		v.openURL(viewerProjectURL)
	}
	if m.open || m.about || m.unlockOpen {
		if ev, ok := gtx.Event(key.Filter{Name: key.NameEscape}); ok {
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				switch {
				case m.unlockOpen:
					m.clearUnlock()
					gtx.Execute(key.FocusCmd{})
				case m.backgroundPicking:
					m.backgroundPicking = false
					gtx.Execute(key.FocusCmd{})
				case m.backgroundOpen:
					m.backgroundOpen = false
					gtx.Execute(key.FocusCmd{})
				case m.exportOpen:
					m.exportOpen, m.open = false, false
					gtx.Execute(key.FocusCmd{})
				default:
					m.open, m.about = false, false
				}
			}
		}
	}
}

func aboutLines() []string {
	return []string{"OFD Viewer", "版本：v" + version.Version,
		"OFD 文档查看器", "应用 ID：" + viewerApplicationID, "项目地址：", viewerProjectURL}
}

// 弹层最后绘制，并覆盖输入区域；底层文档不接收弹层内外的点击。
func (v *viewer) layoutMenuOverlay(gtx layout.Context, th *material.Theme) {
	m := &v.menu
	if !m.open && !m.about && !m.unlockOpen {
		return
	}
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	background := gtx
	background.Constraints = layout.Exact(size)
	m.backdrop.Layout(background, func(gtx layout.Context) layout.Dimensions {
		if m.about || m.backgroundPicking || m.exportOpen || m.unlockOpen {
			paint.Fill(gtx.Ops, color.NRGBA{A: 110})
		}
		return layout.Dimensions{Size: size}
	})
	if m.unlockOpen {
		v.layoutUnlockDialog(gtx, th, size)
		return
	}
	if m.about {
		v.layoutAbout(gtx, th, size)
		return
	}
	if m.backgroundPicking {
		v.layoutBackgroundPicker(gtx, th, size)
		return
	}
	if m.exportOpen {
		v.layoutExportDialog(gtx, th, size)
		return
	}
	width := min(gtx.Dp(unit.Dp(200)), size.X)
	height := min(gtx.Dp(unit.Dp(420)), size.Y)
	y := min(gtx.Dp(unit.Dp(56)), max(0, size.Y-height))
	offset := op.Offset(image.Pt(size.X-width, y)).Push(gtx.Ops)
	defer offset.Pop()
	panel := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	defer panel.Pop()
	event.Op(gtx.Ops, &m.panel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	gtx.Constraints = layout.Exact(image.Pt(width, height))
	m.list.Axis = layout.Vertical
	v.toolbarIcons.init()
	if m.backgroundOpen {
		v.layoutBackgroundMenu(gtx, th)
		return
	}
	items := v.viewMenuItems()
	material.List(th, &m.list).Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
		item := items[i]
		if item.disabled {
			gtx = gtx.Disabled()
		}
		return menuItem(th, item.button, item.icon, item.label)(gtx)
	})
}

type viewMenuItem struct {
	label    string
	button   *widget.Clickable
	icon     *widget.Icon
	disabled bool
}

// 常用阅读操作排在前面，较少使用的文件和程序操作排在末尾。
func (v *viewer) viewMenuItems() []viewMenuItem {
	m := &v.menu
	unavailable := v.busy || v.scene == nil
	fitPageLabel, fitWidthLabel := "适合页面", "适合宽度"
	continuous, double, pageControls := "连续阅读：关", "双页显示：关", "显示底部页码：关"
	if v.viewport.mode == fitWhole {
		fitPageLabel = "✓ 适合页面"
	}
	if v.viewport.mode == fitWidth {
		fitWidthLabel = "✓ 适合宽度"
	}
	if v.continuous.enabled {
		continuous = "连续阅读：开"
	}
	if v.continuous.double {
		double = "双页显示：开"
	}
	if !v.hidePageControls {
		pageControls = "显示底部页码：开"
	}
	items := []viewMenuItem{
		{fitPageLabel, &v.fitPageButton, v.toolbarIcons.fitPage, unavailable},
		{fitWidthLabel, &v.fitWidthButton, v.toolbarIcons.fitWidth, unavailable},
		{continuous, &v.continuousButton, v.toolbarIcons.continuous, unavailable},
		{double, &v.doublePageButton, nil, unavailable},
	}
	if v.compactToolbar {
		items = append(items, viewMenuItem{"缩小", &v.zoomOut, v.toolbarIcons.zoomOut, unavailable}, viewMenuItem{"放大", &v.zoomIn, v.toolbarIcons.zoomIn, unavailable})
	}
	items = append(items,
		viewMenuItem{"背景色", &m.backgroundItem, v.toolbarIcons.background, false},
		viewMenuItem{pageControls, &v.pageControlsButton, v.toolbarIcons.pageControls, false},
		viewMenuItem{"导出…", &m.exportItem, nil, unavailable || v.exporting})
	if v.defaultReader != defaultReaderUnavailable {
		label := "设为默认阅读器"
		if v.defaultReader == defaultReaderIsDefault {
			label = "✓ " + label
		}
		items = append(items, viewMenuItem{label, &m.defaultReaderItem, nil, v.defaultReaderBusy})
	}
	return append(items,
		viewMenuItem{"关于", &m.aboutItem, v.toolbarIcons.about, false},
		viewMenuItem{"退出程序", &m.exitItem, v.toolbarIcons.exit, false})
}

func menuItem(th *material.Theme, button *widget.Clickable, icon *widget.Icon, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return material.Clickable(gtx, button, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(8), Top: unit.Dp(12), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if icon == nil {
							return layout.Dimensions{}
						}
						size := min(gtx.Dp(unit.Dp(20)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
						gtx.Constraints = layout.Exact(image.Pt(size, size))
						return icon.Layout(gtx, th.Fg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, material.Body1(th, label).Layout))
			})
		})
	}
}

func (v *viewer) layoutAbout(gtx layout.Context, th *material.Theme, size image.Point) {
	width := max(0, min(gtx.Dp(unit.Dp(380)), size.X-gtx.Dp(unit.Dp(32))))
	height := max(0, min(gtx.Dp(unit.Dp(480)), size.Y-gtx.Dp(unit.Dp(32))))
	offset := op.Offset(image.Pt((size.X-width)/2, (size.Y-height)/2)).Push(gtx.Ops)
	defer offset.Pop()
	panel := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	defer panel.Pop()
	event.Op(gtx.Ops, &v.menu.panel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	gtx.Constraints = layout.Exact(image.Pt(width, height))
	lines := aboutLines()
	if v.menu.aboutIcon == nil {
		icon := loadViewerIcon()
		v.menu.aboutIcon = &icon
	}
	v.menu.aboutList.Axis = layout.Vertical
	material.List(th, &v.menu.aboutList).Layout(gtx, len(lines)+2, func(gtx layout.Context, i int) layout.Dimensions {
		return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if i == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					icon := widget.Image{Src: *v.menu.aboutIcon, Fit: widget.Contain}
					gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(unit.Dp(96)), gtx.Dp(unit.Dp(96))))
					return icon.Layout(gtx)
				})
			}
			i--
			if i == len(lines) {
				return material.Button(th, &v.menu.closeAbout, "关闭").Layout(gtx)
			}
			if i == 0 {
				return layout.Center.Layout(gtx, material.H6(th, lines[i]).Layout)
			}
			if i == len(lines)-1 {
				return material.Clickable(gtx, &v.menu.aboutLink, func(gtx layout.Context) layout.Dimensions {
					label := material.Body1(th, lines[i])
					label.Color = th.ContrastBg
					return label.Layout(gtx)
				})
			}
			return material.Body1(th, lines[i]).Layout(gtx)
		})
	})
}
