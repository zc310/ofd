package main

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/math/fixed"
)

func TestTitlebarOnlyReplacesWaylandClientDecorations(t *testing.T) {
	var titlebar viewerTitlebar
	titlebar.configure(app.Config{Decorated: false})
	if titlebar.custom {
		t.Fatal("非 Wayland 窗口不能启用自绘标题栏")
	}
	titlebar.wayland = true
	titlebar.configure(app.Config{Decorated: true})
	if titlebar.custom {
		t.Fatal("已有服务端装饰时不能重复绘制标题栏")
	}
	titlebar.configure(app.Config{Decorated: false, Mode: app.Windowed})
	if !titlebar.custom {
		t.Fatal("Wayland 客户端装饰应使用支持系统字体的标题栏")
	}
	titlebar.configure(app.Config{Decorated: false, Mode: app.Fullscreen})
	if titlebar.custom {
		t.Fatal("全屏时不应显示标题栏")
	}
	titlebar.configure(app.Config{Decorated: false, Mode: app.Maximized})
	if !titlebar.custom || !titlebar.decorations.Maximized {
		t.Fatal("最大化状态未同步到标题栏")
	}
}

func TestWindowTitleRetainsChineseFilenameAndPage(t *testing.T) {
	v := viewer{pendingOpen: true}
	var title string
	setTitle := func(value string) { title = value }
	scene := &countedScene{Canvas: canvas.New(210, 297)}
	v.acceptResponse(result{scene: scene, filename: "/tmp/中文文件.ofd", page: 0, total: 3}, func() {}, setTitle)
	if !strings.Contains(title, "中文文件.ofd - 1/3") {
		t.Fatalf("打开文档标题丢失中文或页码: %q", title)
	}
	v.page = 1
	v.updateScrollTitle(0, setTitle)
	if !strings.Contains(title, "中文文件.ofd - 2/3") {
		t.Fatalf("滚动后标题未同步: %q", title)
	}
}

func TestTitlebarRendersDistinctChineseGlyphsWithSystemFont(t *testing.T) {
	shaper := text.NewShaper(text.WithCollection(gofont.Collection()))
	shaper.LayoutString(text.Parameters{PxPerEm: fixed.I(16), MaxWidth: 200}, "中文")
	first, firstOK := shaper.NextGlyph()
	second, secondOK := shaper.NextGlyph()
	if !firstOK || !secondOK || first.ID == second.ID {
		t.Skip("系统未提供覆盖测试汉字的中文字体")
	}
	window, err := headless.NewWindow(400, 50)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	th := material.NewTheme()
	th.Shaper = shaper
	titlebar := viewerTitlebar{custom: true}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(400, 50)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	render := func(title string) []byte {
		t.Helper()
		ops.Reset()
		titlebar.title = title
		titlebar.layout(gtx, th)
		if err := window.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, 400, 50))
		if err := window.Screenshot(pixels); err != nil {
			t.Fatal(err)
		}
		return pixels.Pix
	}
	if bytes.Equal(render("中"), render("文")) {
		t.Fatal("不同中文标题绘制结果相同，系统字体未用于标题栏")
	}
}

func TestTitlebarButtonsPerformWindowActions(t *testing.T) {
	titlebar := viewerTitlebar{custom: true}
	var config app.Config
	var performed system.Action
	option := func(options ...app.Option) {
		for _, apply := range options {
			apply(unit.Metric{}, &config)
		}
	}
	perform := func(action system.Action) { performed |= action }
	titlebar.decorations.Clickable(system.ActionMinimize).Click()
	titlebar.handle(layout.Context{}, option, perform)
	if config.Mode != app.Minimized {
		t.Fatal("最小化按钮未更新窗口模式")
	}
	titlebar.decorations.Clickable(system.ActionMaximize).Click()
	titlebar.handle(layout.Context{}, option, perform)
	if config.Mode != app.Maximized {
		t.Fatal("最大化按钮未更新窗口模式")
	}
	titlebar.decorations.Maximized = true
	titlebar.decorations.Clickable(system.ActionMaximize).Click()
	titlebar.handle(layout.Context{}, option, perform)
	if config.Mode != app.Windowed {
		t.Fatal("再次点击最大化应还原窗口")
	}
	titlebar.decorations.Clickable(system.ActionClose).Click()
	titlebar.handle(layout.Context{}, option, perform)
	if performed&system.ActionClose == 0 {
		t.Fatal("关闭按钮未请求关闭窗口")
	}
}

func TestTitlebarPreservesWindowMoveArea(t *testing.T) {
	titlebar := viewerTitlebar{custom: true, title: "中文文档.ofd - OFD 阅读器 Gio"}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(600, 100)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	dims := titlebar.layout(gtx, continuousTestTheme())
	if dims.Size.Y <= 0 {
		t.Fatal("客户端标题栏未布局")
	}
	router.Frame(&ops)
	if action, ok := router.ActionAt(f32.Pt(20, 20)); !ok || action != system.ActionMove {
		t.Fatalf("标题栏没有窗口拖动区域: action=%v ok=%v", action, ok)
	}
}
