package main

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// 文档背景色：预设和 WASM 阅读器保持同一套
// （cmd/ofd-wasm/web/viewer.js 的 documentBackgroundThemes 与
// setDocumentBackground），桌面端再补一个"自定义"取色器，两个阅读器的手感才
// 不会分叉。页面按透明背景渲染，纸张颜色由页面矩形提供，因此切换背景不需要
// 重新渲染页面。

const (
	documentBackgroundModeKey  = "document-background-mode"
	documentBackgroundColorKey = "document-background-color"
)

// documentBackgroundCustomLabel 是自定义项的菜单文字。WASM 端是取色器输入框，
// 桌面端用 Fyne 的取色对话框。
const documentBackgroundCustomLabel = "自定义…"

// documentBackgroundPreset 是一个可选背景色。Color 为 nil 表示该项由用户取色。
type documentBackgroundPreset struct {
	Key   string
	Label string
	Color color.Color
}

// documentBackgroundPresets 的顺序与 WASM 阅读器的下拉框一致。
var documentBackgroundPresets = []documentBackgroundPreset{
	{Key: "white", Label: "白色", Color: color.White},
	{Key: "transparent", Label: "透明", Color: color.Transparent},
	{Key: "adw-dracula", Label: "暗夜紫灰", Color: rgbColor(0x28, 0x2a, 0x36)},
	{Key: "adw-everforest", Label: "晨雾暖沙", Color: rgbColor(0xf3, 0xf1, 0xe5)},
	{Key: "adw-gruvbox", Label: "复古深棕", Color: rgbColor(0x28, 0x28, 0x28)},
	{Key: "adw-nord", Label: "极光钢蓝", Color: rgbColor(0x2e, 0x34, 0x40)},
	{Key: "adw-solarized", Label: "柔光羊皮", Color: rgbColor(0xfd, 0xf6, 0xe3)},
	{Key: "Peninsula-dark", Label: "半岛墨蓝", Color: rgbColor(0x20, 0x25, 0x2b)},
	{Key: "Plano2", Label: "晴空浅灰", Color: rgbColor(0xee, 0xf2, 0xf5)},
	{Key: "custom", Label: documentBackgroundCustomLabel},
}

func rgbColor(r, g, b uint8) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: 0xff}
}

// documentBackgroundPresetByKey 返回指定预设。未知 key 返回 false。
func documentBackgroundPresetByKey(key string) (documentBackgroundPreset, bool) {
	for _, preset := range documentBackgroundPresets {
		if preset.Key == key {
			return preset, true
		}
	}
	return documentBackgroundPreset{}, false
}

// normalizeDocumentBackgroundMode 把未知或空 key 收敛到白色。偏好里的值可能
// 来自旧版本或被手工改坏，静默套用一个不存在的预设会让背景变成黑色。
func normalizeDocumentBackgroundMode(mode string) string {
	if _, ok := documentBackgroundPresetByKey(mode); ok {
		return mode
	}
	return "white"
}

// resolveDocumentBackground 返回背景色的实际取值：预设取预设色，自定义取用户
// 取的颜色，取色无效时回退白色。透明预设返回透明，页面区域直接露出窗口背景。
func resolveDocumentBackground(mode string, custom color.Color) color.Color {
	mode = normalizeDocumentBackgroundMode(mode)
	if mode != "custom" {
		preset, _ := documentBackgroundPresetByKey(mode)
		if preset.Color != nil {
			return preset.Color
		}
	}
	if custom == nil {
		return color.White
	}
	return custom
}

// documentBackgroundHex 把颜色写成 #rrggbb（带透明度时为 #rrggbbaa），便于存进
// 偏好。格式与 Fyne 取色器自己记录最近使用颜色的写法一致。
func documentBackgroundHex(value color.Color) string {
	normal := color.NRGBAModel.Convert(value).(color.NRGBA)
	if normal.A == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", normal.R, normal.G, normal.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", normal.R, normal.G, normal.B, normal.A)
}

// parseDocumentBackgroundHex 解析偏好里的颜色。格式不符时返回 false，不能把解析
// 失败的字符串当成颜色使用。
func parseDocumentBackgroundHex(value string) (color.Color, bool) {
	if value == "" || value[0] != '#' || (len(value) != 7 && len(value) != 9) {
		return nil, false
	}
	parsed := color.NRGBA{A: 0xff}
	channels := 3
	if len(value) == 9 {
		channels = 4
	}
	for i := 0; i < channels; i++ {
		high, ok := hexDigit(value[1+i*2])
		if !ok {
			return nil, false
		}
		low, ok := hexDigit(value[2+i*2])
		if !ok {
			return nil, false
		}
		switch i {
		case 0:
			parsed.R = high<<4 | low
		case 1:
			parsed.G = high<<4 | low
		case 2:
			parsed.B = high<<4 | low
		case 3:
			parsed.A = high<<4 | low
		}
	}
	return parsed, true
}

func hexDigit(digit byte) (uint8, bool) {
	switch {
	case digit >= '0' && digit <= '9':
		return digit - '0', true
	case digit >= 'a' && digit <= 'f':
		return digit - 'a' + 10, true
	case digit >= 'A' && digit <= 'F':
		return digit - 'A' + 10, true
	}
	return 0, false
}

// loadDocumentBackground 读取记住的背景色。偏好不可用时返回白色：多显示一次白
// 底只是不够好看，套用一个坏值会让页面整片发黑。
func loadDocumentBackground() (string, color.Color) {
	current := fyne.CurrentApp()
	if current == nil {
		return "white", nil
	}
	preferences := current.Preferences()
	mode := normalizeDocumentBackgroundMode(preferences.StringWithFallback(documentBackgroundModeKey, "white"))
	custom, ok := parseDocumentBackgroundHex(preferences.StringWithFallback(documentBackgroundColorKey, ""))
	if !ok {
		return mode, nil
	}
	return mode, custom
}

// saveDocumentBackground 记住背景色。Fyne 的偏好由应用退出时统一落盘。
func saveDocumentBackground(mode string, custom color.Color) {
	current := fyne.CurrentApp()
	if current == nil {
		return
	}
	preferences := current.Preferences()
	preferences.SetString(documentBackgroundModeKey, normalizeDocumentBackgroundMode(mode))
	if custom != nil {
		preferences.SetString(documentBackgroundColorKey, documentBackgroundHex(custom))
	}
}

// pageBackground 返回当前应使用的页面背景色。
func (v *viewer) pageBackground() color.Color {
	return resolveDocumentBackground(v.backgroundMode, v.backgroundCustom)
}

// setDocumentBackground 切换背景色并立即生效：已显示的页面帧、已创建的缩略图
// 单元和此后新建的帧都用新颜色。切换不需要重新渲染页面，因为页面本身按透明
// 背景渲染。
func (v *viewer) setDocumentBackground(mode string, custom color.Color) {
	v.backgroundMode = normalizeDocumentBackgroundMode(mode)
	if custom != nil {
		v.backgroundCustom = custom
	}
	saveDocumentBackground(v.backgroundMode, v.backgroundCustom)
	v.applyDocumentBackground()
}

// applyDocumentBackground 把当前背景色写进排版布局和所有已创建的缩略图单元。
func (v *viewer) applyDocumentBackground() {
	fill := v.pageBackground()
	v.pageLayout.background = fill
	for _, frame := range v.pageLayout.frames {
		frame.background.FillColor = fill
		frame.background.Refresh()
	}
	for _, cell := range v.thumbnailCells {
		cell.background.FillColor = fill
		cell.background.Refresh()
	}
	if v.pageContent != nil && len(v.pageLayout.frames) > 0 {
		v.pageContent.Refresh()
	}
}

// documentBackgroundMenuItems 生成"背景色"子菜单。每项带一个色卡图标，用户不必
// 逐个点开试色。背景色与文档状态无关，导出和加载期间也允许切换，因此这里不置
// Disabled。
func (v *viewer) documentBackgroundMenuItems() []*fyne.MenuItem {
	items := make([]*fyne.MenuItem, 0, len(documentBackgroundPresets))
	for _, preset := range documentBackgroundPresets {
		current := preset
		item := fyne.NewMenuItem(current.Label, nil)
		item.Checked = v.backgroundMode == current.Key
		// 自定义项的色卡跟着用户取到的颜色走；还没取过色时不画色卡，避免用一个
		// 假颜色误导。
		swatch := current.Color
		if swatch == nil {
			swatch = v.backgroundCustom
		}
		item.Icon = documentBackgroundSwatch(swatch)
		if current.Key == "custom" {
			item.Action = func() { v.showCustomDocumentBackground() }
		} else {
			item.Action = func() { v.setDocumentBackground(current.Key, nil) }
		}
		items = append(items, item)
	}
	return items
}

// documentBackgroundSwatch 返回表示该背景色的圆角色卡。菜单项只能放图标资源，
// 所以用内联 SVG 画方块；资源名带颜色值，Fyne 按名字缓存资源，同名会串色。
// 透明色画成对角双色块，和白色预设区分得开。
func documentBackgroundSwatch(value color.Color) fyne.Resource {
	if value == nil {
		return nil
	}
	var body, name string
	if value == color.Transparent {
		name = "swatch-transparent.svg"
		body = `<rect x="1" y="1" width="14" height="14" rx="3" fill="#ffffff"/>` +
			`<path d="M1 15 L15 1 L15 15 Z" fill="#c8cdd6"/>` +
			`<rect x="1" y="1" width="14" height="14" rx="3" fill="none" stroke="#7d838d" stroke-width="1"/>`
	} else {
		// SVG 填充只取 RGB：八位十六进制带 alpha 不是所有 SVG 解析器都认，
		// 透明度对色卡本身也没有意义。
		hex := strings.TrimPrefix(documentBackgroundHex(value), "#")[:6]
		name = "swatch-" + hex + ".svg"
		body = fmt.Sprintf(`<rect x="1" y="1" width="14" height="14" rx="3" fill="#%s" stroke="#7d838d" stroke-width="1"/>`, hex)
	}
	return fyne.NewStaticResource(name, []byte(svgIconHead+body+svgIconTail))
}

// showCustomDocumentBackground 打开取色器确认后应用自定义背景色，返回对话框便于
// 测试断言。
func (v *viewer) showCustomDocumentBackground() *dialog.ColorPickerDialog {
	if v.closed.Load() {
		return nil
	}
	picker := dialog.NewColorPicker("自定义背景色", "选择页面背景颜色", v.applyCustomDocumentBackground, v.window)
	// Fyne 的简易取色器只有固定调色板和灰阶，选不了任意颜色；Advanced 才有色轮和
	// RGB/HSL/Alpha 通道，SetColor 也只在 Advanced 下有效。初值取当前生效的颜色，
	// 重新打开时能接着上次调的位置。
	picker.Advanced = true
	picker.SetColor(v.pageBackground())
	picker.Show()
	return picker
}

// applyCustomDocumentBackground 应用取色器的结果。
func (v *viewer) applyCustomDocumentBackground(value color.Color) {
	if value == nil || v.closed.Load() {
		return
	}
	v.setDocumentBackground("custom", value)
}
