package main

import (
	"strings"
	"testing"
)

// TestViewModeIcons 守住四种视图模式各有互不相同、能被识别为 SVG 的图标。
//
// Fyne 自带图标只有"放大/缩小/全屏"这类粗粒度形状，解释不了"适应宽度"和"适应高度"
// 的区别，因此这里用手绘的内联 SVG：四个图标必须真的不一样，否则用户只能靠文字区分。
func TestViewModeIcons(t *testing.T) {
	modes := []struct {
		mode  pageViewMode
		label string
	}{
		{viewFitPage, viewFitPageLabel},
		{viewFitWidth, viewFitWidthLabel},
		{viewFitHeight, viewFitHeightLabel},
		{viewDoublePage, viewDoublePageLabel},
	}
	names := map[string]string{}
	bodies := map[string]string{}
	for _, item := range modes {
		icon := viewModeIcon(item.mode)
		if icon == nil {
			t.Fatalf("%s 缺少图标", item.label)
		}
		content := string(icon.Content())
		if !strings.HasPrefix(content, "<svg ") || !strings.HasSuffix(content, "</svg>") {
			t.Errorf("%s 的图标外壳不对：%q", item.label, icon.Name())
		}
		// 解析器靠 viewBox 把 16×16 的图形缩放到菜单项的图标尺寸，缺了就画不出来。
		if !strings.Contains(content, `viewBox="0 0 16 16"`) {
			t.Errorf("%s 的图标缺少 viewBox", item.label)
		}
		if !strings.Contains(content, "<rect") && !strings.Contains(content, "<path") {
			t.Errorf("%s 的图标里没有图形", item.label)
		}
		// StaticResource 按资源名缓存，同名不同图会拿到上一次的图标。
		if other, ok := names[icon.Name()]; ok {
			t.Errorf("%s 与 %s 的图标同名 %q，会被缓存串图", item.label, other, icon.Name())
		}
		if other, ok := bodies[content]; ok {
			t.Errorf("%s 与 %s 的图标图形完全相同", item.label, other)
		}
		names[icon.Name()] = item.label
		bodies[content] = item.label
	}
	if icon := viewModeIcon(pageViewMode(99)); icon != nil {
		t.Errorf("未知视图模式不应返回图标，却拿到 %q", icon.Name())
	}
}
