package main

import (
	"image"
	"slices"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/version"
)

func TestAboutShowsRuntimeVersionAndProjectURL(t *testing.T) {
	lines := aboutLines()
	if !slices.Contains(lines, "版本：v"+version.Version) || !slices.Contains(lines, viewerProjectURL) {
		t.Fatalf("关于内容不完整: %v", lines)
	}
}

func TestViewMenuPrioritizesReadingActionsAndKeepsDisabledStates(t *testing.T) {
	for _, compact := range []bool{false, true} {
		v := viewer{compactToolbar: compact}
		items := v.viewMenuItems()
		buttons := make([]*widget.Clickable, 0, len(items))
		for _, item := range items {
			buttons = append(buttons, item.button)
		}
		if !slices.Equal(buttons[:4], []*widget.Clickable{&v.fitPageButton, &v.fitWidthButton, &v.continuousButton, &v.doublePageButton}) {
			t.Fatalf("常用阅读项顺序错误: %v", buttons)
		}
		if !slices.Equal(buttons[len(buttons)-3:], []*widget.Clickable{&v.menu.exportItem, &v.menu.aboutItem, &v.menu.exitItem}) {
			t.Fatal("导出、关于和退出应排在末尾")
		}
		for _, item := range items {
			if item.button == &v.fitPageButton || item.button == &v.doublePageButton || item.button == &v.menu.exportItem {
				if !item.disabled {
					t.Fatalf("无文档时 %s 应禁用", item.label)
				}
			}
			if item.button == &v.pageControlsButton || item.button == &v.menu.aboutItem {
				if item.disabled {
					t.Fatalf("无文档时 %s 应可用", item.label)
				}
			}
		}
	}
}

func TestOpeningMenuReturnsToCommonActions(t *testing.T) {
	var v viewer
	v.menu.list.Position.First = 5
	v.menu.button.Click()
	v.handleMenu(layout.Context{}, func() {})
	if !v.menu.open || v.menu.list.Position.First != 0 {
		t.Fatal("重新打开菜单应回到顶部")
	}
}

func TestRightMenuFitWidthChangesAndPersistsMode(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, total: 3}
	v.menu.open = true
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	settings := viewerSettings{Fit: "page"}
	saved := 0
	frame := func() {
		ops.Reset()
		v.handleMenu(gtx, func() {})
		v.handleScaleButtons(gtx, &settings, func() { saved++ })
		v.layout(gtx, continuousTestTheme())
		router.Frame(&ops)
	}
	frame()
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(700, 125)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(700, 125)})
	frame()
	if v.viewport.mode != fitWidth || settings.Fit != "width" || saved != 1 || v.menu.open {
		t.Fatalf("菜单适应宽度未生效: mode=%v settings=%q saved=%d open=%v", v.viewport.mode, settings.Fit, saved, v.menu.open)
	}
}

func TestRightMenuOpensAboutAndBackdropDismisses(t *testing.T) {
	var v viewer
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	exits := 0
	underlyingOpens := 0
	frame := func() {
		ops.Reset()
		v.handleMenu(gtx, func() { exits++ })
		v.handleKeys(gtx, func(request) {}, func() { exits++ })
		if v.open.Clicked(gtx) {
			underlyingOpens++
		}
		v.layout(gtx, th)
		router.Frame(&ops)
	}
	click := func(x, y float32) {
		router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, y)})
		router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(x, y)})
		frame()
	}
	frame()
	click(772, 28)
	if !v.menu.open {
		t.Fatal("右侧菜单按钮未打开菜单")
	}
	v.menu.aboutItem.Click()
	frame()
	if v.menu.open || !v.menu.about {
		t.Fatal("点击关于未打开关于窗口")
	}
	click(400, 160)
	if !v.menu.about {
		t.Fatal("点击关于面板内部不应关闭窗口")
	}
	click(40, 28)
	if v.menu.open || v.menu.about || exits != 0 || underlyingOpens != 0 {
		t.Fatalf("点击遮罩应仅关闭窗口: menu=%v about=%v exits=%d opens=%d", v.menu.open, v.menu.about, exits, underlyingOpens)
	}
}

func TestEscapeDismissesAboutWithoutExiting(t *testing.T) {
	var v viewer
	v.menu.about = true
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	exits := 0
	frame := func() {
		ops.Reset()
		v.handleMenu(gtx, func() { exits++ })
		v.handleKeys(gtx, func(request) {}, func() { exits++ })
		v.layout(gtx, continuousTestTheme())
		router.Frame(&ops)
	}
	frame()
	router.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	frame()
	if v.menu.about || exits != 0 {
		t.Fatalf("Esc 应关闭关于窗口而不是退出: about=%v exits=%d", v.menu.about, exits)
	}
}

func TestMenuExitInvokesWindowClose(t *testing.T) {
	var v viewer
	v.menu.open = true
	v.menu.exitItem.Click()
	exits := 0
	v.handleMenu(layout.Context{}, func() { exits++ })
	if exits != 1 || v.menu.open {
		t.Fatalf("退出菜单项错误: exits=%d open=%v", exits, v.menu.open)
	}
}

func TestViewMenuTogglesPageControlsAndContinuous(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, total: 3}
	var settings viewerSettings
	saved := 0
	submit := func(request) {}
	persist := func() { saved++ }
	v.menu.open = true
	v.pageControlsButton.Click()
	v.handleViewMenu(layout.Context{}, &settings, persist, submit)
	if !v.hidePageControls || !settings.HidePageControls || v.menu.open || saved != 1 {
		t.Fatal("隐藏页码栏未保存或菜单未关闭")
	}
	v.pageControlsButton.Click()
	v.handleViewMenu(layout.Context{}, &settings, persist, submit)
	if v.hidePageControls || settings.HidePageControls || saved != 2 {
		t.Fatal("显示页码栏未保存")
	}
	v.continuousButton.Click()
	v.handleViewMenu(layout.Context{}, &settings, persist, submit)
	if !v.continuous.enabled || !settings.Continuous || saved != 3 {
		t.Fatal("连续阅读未启用或保存")
	}
}

func TestPageControlsMenuItemReceivesPointerWithoutDocument(t *testing.T) {
	var v viewer
	v.menu.open = true
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	var settings viewerSettings
	frame := func() {
		ops.Reset()
		v.handleMenu(gtx, func() {})
		v.handleViewMenu(gtx, &settings, func() {}, func(request) {})
		v.layout(gtx, continuousTestTheme())
		router.Frame(&ops)
	}
	frame()
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(700, 300)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(700, 300)})
	frame()
	if !v.hidePageControls || !settings.HidePageControls || v.menu.open {
		t.Fatalf("无文档时无法通过菜单隐藏页码栏: hidden=%v settings=%v menu=%v", v.hidePageControls, settings.HidePageControls, v.menu.open)
	}
}

func TestCompactMenuZoomButtonReceivesPointer(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, total: 3}
	v.viewport.scale = 4
	v.menu.open = true
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(400, 700)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	var settings viewerSettings
	frame := func() {
		ops.Reset()
		v.handleMenu(gtx, func() {})
		v.handleScaleButtons(gtx, &settings, func() {})
		v.layout(gtx, continuousTestTheme())
		router.Frame(&ops)
	}
	frame()
	before := v.viewport.scale
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(300, 275)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(300, 275)})
	frame()
	if v.viewport.scale >= before || v.menu.open {
		t.Fatalf("窄窗口菜单缩小未生效: before=%v after=%v open=%v", before, v.viewport.scale, v.menu.open)
	}
}
