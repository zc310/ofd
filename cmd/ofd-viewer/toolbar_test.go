package main

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestToolbarIconsAreValidAndReusedPerViewer(t *testing.T) {
	var v viewer
	v.toolbarIcons.init()
	first := v.toolbarIcons.open
	if first == nil || v.toolbarIcons.recent == nil || v.toolbarIcons.previous == nil || v.toolbarIcons.next == nil || v.toolbarIcons.zoomOut == nil || v.toolbarIcons.zoomIn == nil {
		t.Fatal("工具栏 Material 图标加载失败")
	}
	v.toolbarIcons.init()
	if v.toolbarIcons.open != first {
		t.Fatal("每帧不应重新解析和创建图标")
	}
}

func TestAndroidToolbarHidesRecentFilesButton(t *testing.T) {
	for _, goos := range []string{"android", "linux"} {
		t.Run(goos, func(t *testing.T) {
			v := viewer{scene: &countedScene{Canvas: canvas.New(100, 100)}}
			var router input.Router
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(500, 80)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			v.layoutToolbarContentForOS(gtx, continuousTestTheme(), goos)
			router.Frame(&ops)
			router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(85, 35)})
			router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(85, 35)})
			recent := v.recentButton.Clicked(gtx)
			thumbnail := v.thumbnailButton.Clicked(gtx)
			if goos == "android" && (recent || !thumbnail) {
				t.Fatalf("Android 应隐藏最近文件并让缩略图按钮前移: recent=%v thumbnail=%v", recent, thumbnail)
			}
			if goos != "android" && (!recent || thumbnail) {
				t.Fatalf("桌面应保留最近文件按钮: recent=%v thumbnail=%v", recent, thumbnail)
			}
		})
	}
}

func TestAndroidToolbarDocumentNameTracksOpenAndClose(t *testing.T) {
	v := viewer{}
	if v.toolbarDocumentName() != "" {
		t.Fatal("没有打开文档时不应显示标题")
	}
	v.total = 2
	v.thumbnails.filename = "/documents/很长的文档名称.ofd"
	if got := v.toolbarDocumentName(); got != "很长的文档名称.ofd" {
		t.Fatalf("工具栏应显示文件名而非完整路径: %q", got)
	}
	v.busy, v.scene = true, nil
	if v.toolbarDocumentName() == "" {
		t.Fatal("加载页面时应保留标题")
	}
	v.closeDocument(func(request) {})
	if v.toolbarDocumentName() != "" {
		t.Fatal("关闭文档后不应保留标题")
	}
}

func TestAndroidLongDocumentTitleDoesNotDisplaceToolbarButtons(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(100, 100)}, total: 1, compactToolbar: true}
	v.thumbnails.filename = "这是一个非常长的文件名称这是一个非常长的文件名称.ofd"
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(220, 80)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutToolbarContentForOS(gtx, continuousTestTheme(), "android")
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(85, 35)})
	router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(85, 35)})
	if !v.thumbnailButton.Clicked(gtx) {
		t.Fatal("长标题不能挤掉缩略图按钮")
	}
}

func TestMenuAndToolbarIconsHaveSameSize(t *testing.T) {
	var v viewer
	v.toolbarIcons.init()
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Constraints{Max: image.Pt(200, 100)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	menu := toolbarSquareButton(th, &v.menu.button, v.toolbarIcons.menu, "菜单", false, false)(gtx)
	other := toolbarSquareButton(th, &v.open, v.toolbarIcons.open, "打开", false, false)(gtx)
	if menu.Size != other.Size || menu.Size.X < 48 || menu.Size.Y < 48 {
		t.Fatalf("菜单与工具栏按钮尺寸不一致: 菜单=%v 打开=%v", menu.Size, other.Size)
	}
}

func TestZoomIndicatorKeepsWidthAcrossPageScaleChanges(t *testing.T) {
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Constraints{Max: image.Pt(200, 60)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	for _, text := range []string{"—", "9%", "99%", "100%", "800%", "1000%"} {
		ops.Reset()
		if dims := zoomIndicator(th, text)(gtx); dims.Size.X != 72 {
			t.Fatalf("缩放比例 %q 导致布局宽度变化: %v", text, dims.Size)
		}
	}
}

func TestZoomLabelRetainsScaleDuringPageLoading(t *testing.T) {
	v := viewer{total: 3, busy: true}
	v.viewport.scale = 96.0 / 25.4
	if got := v.zoomLabel(1); got != "100%" {
		t.Fatalf("页面加载时应保留缩放比例，得到 %q", got)
	}
	v.total = 0
	if got := v.zoomLabel(1); got != "—" {
		t.Fatalf("没有文档时不应显示旧比例，得到 %q", got)
	}
}

func TestToolbarSquareButtonRendersNeutralAndSelectedStates(t *testing.T) {
	window, err := headless.NewWindow(48, 48)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var icons toolbarIcons
	icons.init()
	var button widget.Clickable
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(48, 48)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	render := func(active bool) color.RGBA {
		t.Helper()
		ops.Reset()
		toolbarSquareButton(continuousTestTheme(), &button, icons.thumbnail, "缩略图", false, active)(gtx)
		if err := window.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, 48, 48))
		if err := window.Screenshot(pixels); err != nil {
			t.Fatal(err)
		}
		return pixels.RGBAAt(8, 8)
	}
	normal, selected := render(false), render(true)
	if normal.R < 200 || normal.G < 200 || selected.B <= selected.R || normal == selected {
		t.Fatalf("工具栏按钮应使用中性色和不同的选中背景: normal=%v selected=%v", normal, selected)
	}
}

func TestToolbarIconAndTextShareClickTarget(t *testing.T) {
	var icons toolbarIcons
	icons.init()
	var button widget.Clickable
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Constraints{Max: image.Pt(200, 100)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	widget := toolbarIconButton(th, &button, icons.open, "打开", false, false)
	widget(gtx)
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(20, 19)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(20, 19)})
	if !button.Clicked(gtx) {
		t.Fatal("点击图标区域未触发按钮")
	}
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(45, 19)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(45, 19)})
	if !button.Clicked(gtx) {
		t.Fatal("点击文字区域未触发按钮")
	}
}

func TestToolbarIconAppearsOnGPU(t *testing.T) {
	window, err := headless.NewWindow(160, 60)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var icons toolbarIcons
	icons.init()
	var button widget.Clickable
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Constraints{Max: image.Pt(160, 60)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	toolbarIconButton(continuousTestTheme(), &button, icons.open, "Open", false, false)(gtx)
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 160, 60))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	light := 0
	for y := 10; y < 28; y++ {
		for x := 12; x < 30; x++ {
			c := pixels.RGBAAt(x, y)
			if c.R > 220 && c.G > 220 && c.B > 220 {
				light++
			}
		}
	}
	if light < 10 {
		t.Fatalf("工具栏图标未绘制: light=%d", light)
	}
}

func TestToolbarFlowWrapsWithoutLosingClickTarget(t *testing.T) {
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Constraints{Max: image.Pt(140, 200)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	buttons := make([]widget.Clickable, 3)
	items := make([]layout.Widget, len(buttons))
	for i := range buttons {
		items[i] = func(gtx layout.Context) layout.Dimensions {
			return material.Clickable(gtx, &buttons[i], func(_ layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(60, 20)}
			})
		}
	}
	dims := toolbarFlow(gtx, 5, items...)
	if dims.Size != image.Pt(140, 45) {
		t.Fatalf("工具栏换行尺寸错误: %v", dims.Size)
	}
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(30, 35)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(30, 35)})
	if !buttons[2].Clicked(gtx) || buttons[0].Clicked(gtx) || buttons[1].Clicked(gtx) {
		t.Fatal("换行后点击没有命中对应控件")
	}
}

func TestToolbarSwitchesToCompactAtNarrowWindowWidth(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, page: 1, total: 3}
	th := continuousTestTheme()
	measure := func(width int) image.Point {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Constraints: layout.Constraints{Max: image.Pt(width, 1000)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		return v.layoutHeader(gtx, th).Size
	}
	wide, narrow := measure(1200), measure(320)
	if wide.X != 1200 || narrow.X != 320 || narrow.Y > wide.Y || !v.compactToolbar {
		t.Fatalf("工具栏没有适应窗口宽度: wide=%v narrow=%v", wide, narrow)
	}
}

func TestToolbarFitsInShortWindow(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, total: 3}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Constraints{Max: image.Pt(200, 240)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	dims := v.layoutToolbar(gtx, th)
	if dims.Size.Y > 120 {
		t.Fatalf("工具栏不应占满矮窗口: %v", dims.Size)
	}
}

func TestRecentButtonDoesNotToggleWhileBusy(t *testing.T) {
	v := viewer{busy: true}
	v.recentButton.Click()
	v.handleRecent(layout.Context{}, func() {}, func(request) {})
	if v.showRecent {
		t.Fatal("加载期间不应打开最近文件列表")
	}
}

func TestFloatingPageControlsRoutePreviousAndNextClicks(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, page: 1, total: 3}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Constraints{Max: image.Pt(400, 100)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	dims := v.layoutPageControls(gtx, continuousTestTheme())
	router.Frame(&ops)
	click := func(x float32) {
		router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, 20)})
		router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(x, 20)})
	}
	click(20)
	if !v.previous.Clicked(gtx) {
		t.Fatal("浮动栏上一页图标不可点击")
	}
	click(float32(dims.Size.X - 20))
	if !v.next.Clicked(gtx) {
		t.Fatal("浮动栏下一页图标不可点击")
	}
}

func TestFloatingPageControlsUseNeutralColors(t *testing.T) {
	window, err := headless.NewWindow(360, 80)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, page: 1, total: 3}
	v.pageInput.SetText("2")
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Constraints{Max: image.Pt(360, 80)}, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	dims := v.layoutPageControls(gtx, continuousTestTheme())
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 360, 80))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	blue := 0
	for y := range dims.Size.Y {
		for x := range dims.Size.X {
			c := pixels.RGBAAt(x, y)
			if c.A > 200 && int(c.B)-int(c.R) > 45 {
				blue++
			}
		}
	}
	if blue > 20 {
		t.Fatalf("浮动栏不应出现大面积蓝色按钮: pixels=%d", blue)
	}
}

func TestScaleButtonsIgnoreBusyAndSaveFitPreference(t *testing.T) {
	v := viewer{busy: true, scene: &countedScene{Canvas: canvas.New(210, 297)}}
	settings := viewerSettings{Fit: "page"}
	saved := 0
	persist := func() { saved++ }
	v.fitWidthButton.Click()
	v.handleScaleButtons(layout.Context{}, &settings, persist)
	if v.viewport.mode != fitWhole || settings.Fit != "page" || saved != 0 {
		t.Fatal("加载期间不应修改适应模式和偏好")
	}
	v.busy = false
	v.fitWidthButton.Click()
	v.handleScaleButtons(layout.Context{}, &settings, persist)
	if v.viewport.mode != fitWidth || settings.Fit != "width" || saved != 1 {
		t.Fatal("适应宽度按钮未更新模式和偏好")
	}
}
