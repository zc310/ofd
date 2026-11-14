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
	"gioui.org/x/colorpicker"
)

type backgroundPreset struct {
	key, label string
	color      color.NRGBA
}

func (v *viewer) layoutBackgroundMenu(gtx layout.Context, th *material.Theme) {
	m := &v.menu
	m.backgroundList.Axis = layout.Vertical
	material.List(th, &m.backgroundList).Layout(gtx, len(backgroundPresets)+2, func(gtx layout.Context, i int) layout.Dimensions {
		if i == 0 {
			return menuItem(th, &m.backgroundBack, nil, "‹ 返回菜单")(gtx)
		}
		if i <= len(backgroundPresets) {
			preset := backgroundPresets[i-1]
			return material.Clickable(gtx, &m.backgroundButtons[i-1], func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							size := min(gtx.Dp(unit.Dp(20)), gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
							paint.FillShape(gtx.Ops, color.NRGBA{R: 160, G: 160, B: 160, A: 255}, clip.Rect{Max: image.Pt(size, size)}.Op())
							if size > 2 {
								paint.FillShape(gtx.Ops, preset.color, clip.Rect{Min: image.Pt(1, 1), Max: image.Pt(size-1, size-1)}.Op())
							}
							return layout.Dimensions{Size: image.Pt(size, size)}
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							label := preset.label
							if v.background == preset.key || (v.background == "" && preset.key == "white") {
								label = "✓ " + label
							}
							return material.Body1(th, label).Layout(gtx)
						}))
				})
			})
		}
		label := "自定义颜色…"
		if v.background == "custom" {
			label = "✓ " + label
		}
		return menuItem(th, &m.backgroundCustomItem, v.toolbarIcons.background, label)(gtx)
	})
}

// 独立弹层给滑块留出空间，矮窗口中可纵向滚动。
func (v *viewer) layoutBackgroundPicker(gtx layout.Context, th *material.Theme, size image.Point) {
	width := max(0, min(gtx.Dp(unit.Dp(460)), size.X-gtx.Dp(unit.Dp(24))))
	height := max(0, min(gtx.Dp(unit.Dp(340)), size.Y-gtx.Dp(unit.Dp(24))))
	offset := op.Offset(image.Pt((size.X-width)/2, (size.Y-height)/2)).Push(gtx.Ops)
	defer offset.Pop()
	panel := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	defer panel.Pop()
	event.Op(gtx.Ops, &v.menu.panel)
	paint.Fill(gtx.Ops, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	gtx.Constraints = layout.Exact(image.Pt(width, height))
	v.menu.backgroundPickerList.Axis = layout.Vertical
	material.List(th, &v.menu.backgroundPickerList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(material.H6(th, "自定义背景色").Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if gtx.Constraints.Max.X < gtx.Dp(unit.Dp(220)) {
						return material.Body1(th, "窗口过窄，请加宽后选择颜色。").Layout(gtx)
					}
					return colorpicker.Picker(th, &v.menu.backgroundPicker, "颜色").Layout(gtx)
				}),
				layout.Rigid(material.Caption(th, v.menu.backgroundError).Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(material.Button(th, &v.menu.backgroundCancel, "取消").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(material.Button(th, &v.menu.backgroundApply, "应用").Layout))
				}))
		})
	})
}

// 与 Fyne 和 WASM 阅读器使用相同的纸张预设。
var backgroundPresets = [...]backgroundPreset{ //nolint:gochecknoglobals // 固定预设由菜单和绘制共享，不在运行时修改。
	{"white", "白色", color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
	{"transparent", "透明", color.NRGBA{}},
	{"adw-dracula", "暗夜紫灰", color.NRGBA{R: 0x28, G: 0x2a, B: 0x36, A: 255}},
	{"adw-everforest", "晨雾暖沙", color.NRGBA{R: 0xf3, G: 0xf1, B: 0xe5, A: 255}},
	{"adw-gruvbox", "复古深棕", color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 255}},
	{"adw-nord", "极光钢蓝", color.NRGBA{R: 0x2e, G: 0x34, B: 0x40, A: 255}},
	{"adw-solarized", "柔光羊皮", color.NRGBA{R: 0xfd, G: 0xf6, B: 0xe3, A: 255}},
	{"Peninsula-dark", "半岛墨蓝", color.NRGBA{R: 0x20, G: 0x25, B: 0x2b, A: 255}},
	{"Plano2", "晴空浅灰", color.NRGBA{R: 0xee, G: 0xf2, B: 0xf5, A: 255}},
	{"mist-blue", "浅雾蓝", color.NRGBA{R: 0xed, G: 0xf5, B: 0xfc, A: 255}},
	{"mint-green", "薄荷浅绿", color.NRGBA{R: 0xed, G: 0xf8, B: 0xf1, A: 255}},
	{"blush-pink", "樱花浅粉", color.NRGBA{R: 0xff, G: 0xf1, B: 0xf3, A: 255}},
	{"lavender", "薰衣草浅紫", color.NRGBA{R: 0xf5, G: 0xf0, B: 0xfc, A: 255}},
}

func parseBackgroundColor(value string) (color.NRGBA, bool) {
	if !strings.HasPrefix(value, "#") || (len(value) != 7 && len(value) != 9) {
		return color.NRGBA{}, false
	}
	for _, c := range value[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return color.NRGBA{}, false
		}
	}
	n, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	if len(value) == 7 {
		n = n<<8 | 255
	}
	return color.NRGBA{R: uint8(n >> 24), G: uint8(n >> 16), B: uint8(n >> 8), A: uint8(n)}, true
}

func paperColor(mode, custom string) color.NRGBA {
	if mode == "custom" {
		if c, ok := parseBackgroundColor(custom); ok {
			return c
		}
	}
	for _, preset := range backgroundPresets {
		if preset.key == mode {
			return preset.color
		}
	}
	return backgroundPresets[0].color
}
