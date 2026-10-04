package main

import (
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"testing"

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
	if got := resolveDocumentBackground("custom", nil); !sameBackground(got, color.White) {
		t.Errorf("自定义色缺失时 = %v，期望白色", got)
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
