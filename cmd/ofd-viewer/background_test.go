package main

import (
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// newBackgroundTestViewer 构造带窗口的查看器，只用于背景色相关的用例。应用由
// 调用方创建：偏好要在同一个应用实例内共享，才能验证"记住"的行为。
func newBackgroundTestViewer(t *testing.T) *viewer {
	t.Helper()
	window := test.NewWindow(nil)
	t.Cleanup(window.Close)
	v := newViewer(window)
	window.SetContent(v.content)
	return v
}

// wasmDocumentBackgroundThemes 从 WASM 阅读器脚本里读出预设表。两个阅读器的
// 预设必须一致：桌面端单独维护一份时，两边迟早会漂移。
func wasmDocumentBackgroundThemes(t *testing.T) map[string]string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "ofd-wasm", "web", "viewer.js"))
	if err != nil {
		t.Skipf("WASM 阅读器脚本不可用: %v", err)
	}
	block := regexp.MustCompile(`(?s)const documentBackgroundThemes = \{(.*?)\};`).FindSubmatch(source)
	if block == nil {
		t.Fatal("WASM 阅读器里找不到 documentBackgroundThemes")
	}
	entry := regexp.MustCompile(`'?([A-Za-z][A-Za-z0-9_-]*)'?\s*:\s*'(#[0-9a-fA-F]{6})'`)
	themes := map[string]string{}
	for _, match := range entry.FindAllSubmatch(block[1], -1) {
		themes[string(match[1])] = string(match[2])
	}
	if len(themes) == 0 {
		t.Fatal("WASM 阅读器的 documentBackgroundThemes 为空")
	}
	return themes
}

// TestDocumentBackgroundPresetsMatchWASMReader 保护两个阅读器的预设一致。
//
// WASM 端把白色和透明单独处理、不在 documentBackgroundThemes 里，自定义项是
// 取色器而不是固定颜色，因此桌面端只比对主题项，白色/透明/自定义各有断言。
func TestDocumentBackgroundPresetsMatchWASMReader(t *testing.T) {
	themes := wasmDocumentBackgroundThemes(t)
	presets := map[string]string{}
	for _, preset := range documentBackgroundPresets {
		if preset.Color == nil {
			continue
		}
		presets[preset.Key] = documentBackgroundHex(preset.Color)
	}
	delete(presets, "white")
	delete(presets, "transparent")
	if len(presets) != len(themes) {
		t.Fatalf("桌面端主题项 %d 个，WASM 端 %d 个：%v", len(presets), len(themes), presets)
	}
	for key, want := range themes {
		got, ok := presets[key]
		if !ok {
			t.Errorf("桌面端缺少 WASM 端的主题 %s", key)
			continue
		}
		if !equalHex(got, want) {
			t.Errorf("主题 %s = %s，WASM 端为 %s", key, got, want)
		}
	}
	// 白色与透明必须始终存在，且顺序与 WASM 端下拉框一致。
	if len(documentBackgroundPresets) != len(themes)+3 {
		t.Fatalf("预设共 %d 项，期望主题 %d 项加白色、透明和自定义", len(documentBackgroundPresets), len(themes))
	}
	if documentBackgroundPresets[0].Key != "white" || !sameBackground(documentBackgroundPresets[0].Color, color.White) {
		t.Error("第一项应是白色")
	}
	if documentBackgroundPresets[1].Key != "transparent" || documentBackgroundPresets[1].Color != color.Transparent {
		t.Error("第二项应是透明")
	}
	if last := documentBackgroundPresets[len(documentBackgroundPresets)-1]; last.Key != "custom" || last.Color != nil {
		t.Error("最后一项应是取色的自定义项")
	}
}

func equalHex(a, b string) bool {
	return len(a) == len(b) && lowerASCII(a) == lowerASCII(b)
}

func lowerASCII(value string) string {
	out := []byte(value)
	for i, char := range out {
		if char >= 'A' && char <= 'F' {
			out[i] = char + ('a' - 'A')
		}
	}
	return string(out)
}

// TestSetDocumentBackgroundAppliesImmediately 保护切换背景色不需要重新渲染页面：
// 页面按透明背景渲染，改纸张矩形就够。
func TestSetDocumentBackgroundAppliesImmediately(t *testing.T) {
	v := newLoadedTestViewer(t)
	// 前置条件：帧和缩略图单元都存在。
	v.pageLayout.growFrames(2)
	cell := v.newThumbnailCell()
	if !sameBackground(v.pageLayout.frames[0].background.FillColor, color.White) {
		t.Fatalf("初始背景 = %v，期望白色", v.pageLayout.frames[0].background.FillColor)
	}

	v.setDocumentBackground("adw-nord", nil)
	want := resolveDocumentBackground("adw-nord", nil)
	for _, frame := range v.pageLayout.frames {
		if !sameBackground(frame.background.FillColor, want) {
			t.Fatalf("显示帧背景 = %v，期望 %v", frame.background.FillColor, want)
		}
	}
	if !sameBackground(cell.background.FillColor, want) {
		t.Fatalf("缩略图背景 = %v，期望 %v", cell.background.FillColor, want)
	}
	// 此后新建的帧必须继承当前背景色，否则滚动到未创建过的帧会跳回白色。
	v.pageLayout.growFrames(v.pageLayout.frames[len(v.pageLayout.frames)-1].page + 4)
	if last := v.pageLayout.frames[len(v.pageLayout.frames)-1]; !sameBackground(last.background.FillColor, want) {
		t.Fatalf("新建显示帧背景 = %v，期望 %v", last.background.FillColor, want)
	}

	// 透明预设要真的透明：页面区域直接露出窗口背景。
	v.setDocumentBackground("transparent", nil)
	if v.pageLayout.frames[0].background.FillColor != color.Transparent {
		t.Fatalf("透明预设背景 = %v，期望透明", v.pageLayout.frames[0].background.FillColor)
	}
}

// TestCustomDocumentBackgroundSurvivesRestart 保护自定义背景色被记住。
//
// 取色对话框只把颜色交给 setDocumentBackground，真正的持久化由它完成，因此
// 这里直接调用它，再新建查看器验证读取。
func TestCustomDocumentBackgroundSurvivesRestart(t *testing.T) {
	test.NewTempApp(t)
	v := newBackgroundTestViewer(t)
	custom := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	v.setDocumentBackground("custom", custom)
	if !sameBackground(v.pageBackground(), custom) {
		t.Fatalf("自定义背景 = %v，期望 %v", v.pageBackground(), custom)
	}

	restored := newBackgroundTestViewer(t)
	if restored.backgroundMode != "custom" {
		t.Fatalf("重启后模式 = %q，期望 custom", restored.backgroundMode)
	}
	if !sameBackground(restored.pageBackground(), custom) {
		t.Fatalf("重启后背景 = %v，期望 %v", restored.pageBackground(), custom)
	}
}

// TestDocumentBackgroundFallsBackToWhite 保护坏偏好不会让页面整片发黑。
func TestDocumentBackgroundFallsBackToWhite(t *testing.T) {
	if got := normalizeDocumentBackgroundMode("adw-nonexistent"); got != "white" {
		t.Errorf("未知模式归一 = %q，期望 white", got)
	}
	if got := normalizeDocumentBackgroundMode(""); got != "white" {
		t.Errorf("空模式归一 = %q，期望 white", got)
	}
	if _, ok := parseDocumentBackgroundHex("#12345"); ok {
		t.Error("长度不足的十六进制不应解析成功")
	}
	if _, ok := parseDocumentBackgroundHex("#12345g"); ok {
		t.Error("非十六进制字符不应解析成功")
	}
	parsed, ok := parseDocumentBackgroundHex("#123456")
	if !ok || !sameBackground(parsed, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}) {
		t.Fatalf("合法十六进制解析 = %v, %v", parsed, ok)
	}
	// 取色器能选透明度，存进偏好再读回来必须还是半透明，不能悄悄变成不透明。
	half := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x80}
	if hex := documentBackgroundHex(half); hex != "#12345680" {
		t.Errorf("带透明度的颜色写成 %q，期望 #12345680", hex)
	}
	restored, ok := parseDocumentBackgroundHex("#12345680")
	if !ok || !sameBackground(restored, half) {
		t.Errorf("带透明度的颜色解析 = %v, %v，期望 %v", restored, ok, half)
	}
	if _, ok := parseDocumentBackgroundHex("#1234567"); ok {
		t.Error("长度介于两者之间的十六进制不应解析成功")
	}
	if got := resolveDocumentBackground("custom", nil); !sameBackground(got, color.White) {
		t.Errorf("自定义色缺失时 = %v，期望白色", got)
	}
}

// TestMenuEntriesCarryIcons 保护「视图」和「背景色」有图标，且背景色子菜单挂齐。
//
// 图标是菜单里唯一的定位线索，两个无图标条目夹在「导出」和「关于」之间很难扫到。
func TestMenuEntriesCarryIcons(t *testing.T) {
	test.NewTempApp(t)
	v := newBackgroundTestViewer(t)
	menu := v.buildMenu()
	entries := map[string]*fyne.MenuItem{}
	for _, item := range menu.Items {
		entries[item.Label] = item
	}
	for _, label := range []string{"视图", "背景色"} {
		item, ok := entries[label]
		if !ok {
			t.Fatalf("菜单里缺少 %s 项", label)
		}
		if item.Icon == nil {
			t.Errorf("%s 项缺少图标", label)
		}
	}
	view := entries["视图"]
	if view.ChildMenu == nil || len(view.ChildMenu.Items) != 4 {
		t.Fatalf("视图子菜单项 = %v，期望 4 项", view.ChildMenu)
	}
	background := entries["背景色"]
	if background.ChildMenu == nil || len(background.ChildMenu.Items) != len(documentBackgroundPresets) {
		t.Fatalf("背景色子菜单项 = %v，期望 %d 项", background.ChildMenu, len(documentBackgroundPresets))
	}
	if background.Disabled {
		t.Error("背景色项不应被禁用")
	}
}

// TestShowCustomDocumentBackgroundUsesAdvancedPicker 保护"自定义"能真正选颜色。
//
// Fyne 的简易取色器只提供固定调色板和灰阶，选不了任意颜色；Advanced 才有色轮和
// RGB/HSL/Alpha 通道，SetColor 也只在 Advanced 下生效（否则 Fyne 会记一条错误并
// 忽略初值）。
func TestShowCustomDocumentBackgroundUsesAdvancedPicker(t *testing.T) {
	test.NewTempApp(t)
	v := newBackgroundTestViewer(t)
	picker := v.showCustomDocumentBackground()
	if picker == nil {
		t.Fatal("应返回取色对话框")
	}
	if !picker.Advanced {
		t.Error("简易取色器只能选固定调色板，必须打开 Advanced")
	}
	if v.window.Canvas().Overlays().Top() == nil {
		t.Error("取色对话框应显示在窗口上")
	}
	if v.closed.Load() {
		t.Fatal("前置条件失败：查看器不应处于关闭状态")
	}

	// 取色结果要落到背景色上；窗口关闭后不应再改界面。
	picked := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	v.applyCustomDocumentBackground(picked)
	if v.backgroundMode != "custom" || !sameBackground(v.pageBackground(), picked) {
		t.Fatalf("取色结果 = %q / %v，期望 custom / %v", v.backgroundMode, v.pageBackground(), picked)
	}
	v.closed.Store(true)
	v.applyCustomDocumentBackground(color.NRGBA{R: 0x44, G: 0x55, B: 0x66, A: 0xff})
	if !sameBackground(v.pageBackground(), picked) {
		t.Fatal("窗口关闭后不应再应用颜色")
	}
}

// TestDocumentBackgroundMenuItemsProtectSelection 保护菜单与当前选择一致：
// 打勾的必须是当前预设，点一下就切换，自定义项打开取色器。
func TestDocumentBackgroundMenuItemsProtectSelection(t *testing.T) {
	test.NewTempApp(t)
	v := newBackgroundTestViewer(t)
	items := v.documentBackgroundMenuItems()
	if len(items) != len(documentBackgroundPresets) {
		t.Fatalf("菜单项 %d 个，预设 %d 个", len(items), len(documentBackgroundPresets))
	}
	checked := 0
	for _, item := range items {
		if item.Checked {
			checked++
		}
		if item.Disabled {
			t.Errorf("%s 项不应被禁用：背景色与文档状态无关", item.Label)
		}
	}
	if checked != 1 {
		t.Fatalf("初始勾选 %d 项，期望 1 项", checked)
	}

	for _, item := range items {
		if item.Label == "极光钢蓝" {
			item.Action()
		}
	}
	if v.backgroundMode != "adw-nord" {
		t.Fatalf("点击后模式 = %q，期望 adw-nord", v.backgroundMode)
	}
	if !sameBackground(v.pageBackground(), resolveDocumentBackground("adw-nord", nil)) {
		t.Fatalf("点击后背景 = %v", v.pageBackground())
	}
	checked = 0
	for _, item := range v.documentBackgroundMenuItems() {
		if item.Checked {
			checked++
		}
	}
	if checked != 1 {
		t.Fatalf("切换后勾选 %d 项，期望 1 项", checked)
	}

	// 自定义项打开取色对话框；这里只验证它确实打开了弹层，取色回调由 Fyne
	// 内部触发，不在用例里模拟。
	custom := items[len(items)-1]
	if custom.Action == nil {
		t.Fatal("自定义项缺少动作")
	}
	custom.Action()
	if v.window.Canvas().Overlays().Top() == nil {
		t.Error("自定义项应打开取色对话框")
	}
}

// TestDocumentBackgroundSwatchesShowRealColor 保护菜单里的色卡。
//
// 菜单项只能放图标资源，色卡是内联 SVG：必须以 "<svg " 开头或以 .svg 结尾才会
// 被 Fyne 当成 SVG 解析，填充色要和预设一致，资源名要带颜色值——Fyne 按名字缓存
// 资源，同名会把两个色卡画成同一个颜色。
func TestDocumentBackgroundSwatchesShowRealColor(t *testing.T) {
	names := map[string]string{}
	for _, preset := range documentBackgroundPresets {
		if preset.Color == nil {
			continue
		}
		resource := documentBackgroundSwatch(preset.Color)
		if resource == nil {
			t.Fatalf("%s 缺少色卡", preset.Label)
		}
		content := string(resource.Content())
		if !strings.HasPrefix(content, "<svg ") || !strings.HasSuffix(resource.Name(), ".svg") {
			t.Errorf("%s 的色卡不会被识别为 SVG：%q / %q", preset.Label, resource.Name(), content[:min(len(content), 12)])
		}
		// 透明色卡是对角双色块，不含实色；其余色卡的填充色必须就是预设颜色。
		if preset.Color != color.Transparent && !strings.Contains(content, documentBackgroundHex(preset.Color)) {
			t.Errorf("%s 的色卡没有使用预设颜色 %s", preset.Label, documentBackgroundHex(preset.Color))
		}
		if other, ok := names[resource.Name()]; ok {
			t.Errorf("%s 与 %s 的色卡同名 %q，会被缓存串色", preset.Label, other, resource.Name())
		}
		names[resource.Name()] = preset.Label
	}
	// 透明预设不能画成一块实色，否则和白色预设无法区分。
	transparent := documentBackgroundSwatch(color.Transparent)
	if strings.Contains(string(transparent.Content()), documentBackgroundHex(color.Transparent)) {
		t.Error("透明色卡不应画成黑色实心块")
	}
	if documentBackgroundSwatch(nil) != nil {
		t.Error("没有颜色时不应返回色卡")
	}
}

// TestCustomDocumentBackgroundSwatchFollowsPickedColor 保护自定义项的色卡跟着
// 取到的颜色走，用户下次开菜单能直接看到自己选的颜色。
func TestCustomDocumentBackgroundSwatchFollowsPickedColor(t *testing.T) {
	test.NewTempApp(t)
	v := newBackgroundTestViewer(t)
	custom := v.documentBackgroundMenuItems()[len(documentBackgroundPresets)-1]
	if custom.Icon != nil {
		t.Error("还没取过色时不应显示色卡")
	}
	v.setDocumentBackground("custom", color.NRGBA{R: 0x0a, G: 0x0b, B: 0x0c, A: 0xff})
	custom = v.documentBackgroundMenuItems()[len(documentBackgroundPresets)-1]
	if custom.Icon == nil || !strings.Contains(string(custom.Icon.Content()), "#0a0b0c") {
		t.Fatalf("自定义色卡 = %v，期望包含取到的颜色 #0a0b0c", custom.Icon)
	}
}
