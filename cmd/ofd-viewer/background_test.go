package main

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPaperColorPresetsAndCustomValidation(t *testing.T) {
	if got := paperColor("unknown", ""); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("未知预设应回退白色: %v", got)
	}
	if got := paperColor("transparent", ""); got.A != 0 {
		t.Fatalf("透明预设错误: %v", got)
	}
	if got := paperColor("custom", "#12345680"); got != (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x80}) {
		t.Fatalf("自定义颜色错误: %v", got)
	}
	for _, value := range []string{"", "#12", "#zzzzzz", "#123456789", "#+12345"} {
		if _, ok := parseBackgroundColor(value); ok {
			t.Fatalf("不应接受无效颜色 %q", value)
		}
	}
}

func TestBackgroundMenuSelectionAndSettingsRoundTrip(t *testing.T) {
	v := viewer{}
	v.menu.open, v.menu.backgroundOpen = true, true
	var settings viewerSettings
	saved := 0
	v.menu.backgroundButtons[3].Click()
	v.handleViewMenu(layout.Context{}, &settings, func() { saved++ }, func(request) {})
	if v.background != "adw-everforest" || settings.Background != v.background || saved != 1 || v.menu.open {
		t.Fatal("选择预设未生效或未保存")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := saveSettings(path, settings); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil || got.Background != v.background {
		t.Fatalf("背景偏好未恢复: %v %v", got, err)
	}
	v.menu.open, v.menu.backgroundOpen = true, true
	v.menu.backgroundPicking = true
	v.menu.backgroundPicker.SetColor(color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x80})
	v.menu.backgroundPicker.SetText("bad")
	v.menu.backgroundApply.Click()
	v.handleViewMenu(layout.Context{}, &settings, func() { saved++ }, func(request) {})
	if v.menu.backgroundError == "" || saved != 1 || v.background != "adw-everforest" {
		t.Fatal("无效自定义颜色不应修改背景")
	}
	v.menu.backgroundPicker.SetColor(color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x80})
	v.menu.backgroundApply.Click()
	var router input.Router
	v.handleViewMenu(layout.Context{Source: router.Source()}, &settings, func() { saved++ }, func(request) {})
	if v.background != "custom" || settings.BackgroundCustom != "#12345680" || saved != 2 {
		t.Fatal("自定义颜色未保存")
	}
}

func TestBackgroundMenuPointerNavigation(t *testing.T) {
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
	click := func(y float32) {
		router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(700, y)})
		router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(700, y)})
		frame()
	}
	frame()
	click(275)
	if !v.menu.backgroundOpen {
		t.Fatal("背景色菜单入口不可点击")
	}
	click(175)
	if v.background != "transparent" || v.menu.open {
		t.Fatalf("背景色子菜单选择错误: background=%q open=%v", v.background, v.menu.open)
	}
}

func TestBackgroundPickerStartsWithCurrentColorAndCancelKeepsIt(t *testing.T) {
	v := viewer{background: "mint-green"}
	v.menu.open, v.menu.backgroundOpen = true, true
	var router input.Router
	gtx := layout.Context{Source: router.Source()}
	v.menu.backgroundCustomItem.Click()
	v.handleMenu(gtx, func() {})
	if !v.menu.backgroundPicking || v.menu.backgroundPicker.Color() != paperColor("mint-green", "") {
		t.Fatal("取色器应以当前纸张颜色初始化")
	}
	v.menu.backgroundPicker.SetColor(color.NRGBA{R: 200, A: 64})
	v.menu.backgroundCancel.Click()
	v.handleMenu(gtx, func() {})
	if v.menu.backgroundPicking || !v.menu.backgroundOpen || v.background != "mint-green" {
		t.Fatal("取消取色应返回背景菜单并保持原颜色")
	}
}

func TestBackgroundPickerLayoutsAtDifferentWindowSizes(t *testing.T) {
	window, err := headless.NewWindow(500, 400)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var v viewer
	v.menu.open, v.menu.backgroundOpen, v.menu.backgroundPicking = true, true, true
	v.menu.backgroundPicker.SetColor(color.NRGBA{R: 100, G: 120, B: 140, A: 180})
	var router input.Router
	var ops op.Ops
	for _, size := range []image.Point{image.Pt(500, 400), image.Pt(300, 180), image.Pt(180, 120)} {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(size), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		v.layoutMenuOverlay(gtx, continuousTestTheme())
		router.Frame(&ops)
		if err := window.Frame(&ops); err != nil {
			t.Fatalf("窗口 %v 取色器绘制失败: %v", size, err)
		}
	}
}

func TestPaperBackgroundChangesBothReadingModesOnGPU(t *testing.T) {
	window, err := headless.NewWindow(120, 140)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	scene := &countedScene{Canvas: canvas.New(100, 100)}
	v := viewer{background: "mint-green", scene: scene, total: 1, viewport: pageViewport{mode: fitManual, scale: 0.5}}
	v.continuous.reset(0)
	v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{width: 100, height: 100}}}
	v.continuous.entries.AddWeighted(0, &continuousEntry{scene: scene}, 128)
	for _, continuous := range []bool{false, true} {
		ops.Reset()
		if continuous {
			v.layoutContinuous(gtx, continuousTestTheme())
		} else {
			v.layoutViewport(gtx)
		}
		if err := window.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, 120, 140))
		if err := window.Screenshot(pixels); err != nil {
			t.Fatal(err)
		}
		y := 70
		if continuous {
			y = 20
		}
		if got := pixels.RGBAAt(60, y); got != (color.RGBA{R: 0xed, G: 0xf8, B: 0xf1, A: 255}) {
			t.Fatalf("continuous=%v 纸张颜色错误: %v", continuous, got)
		}
	}
}

func TestThumbnailPaperChangesWithoutRegeneratingImage(t *testing.T) {
	window, err := headless.NewWindow(120, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var state thumbnailState
	state.reset("document.ofd")
	state.accept(thumbnailResult{generation: state.generation, page: 0, image: image.NewRGBA(image.Rect(0, 0, 20, 20))})
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 160)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	state.paper = paperColor("mint-green", "")
	state.layout(gtx, continuousTestTheme(), 1, 0, false)
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 120, 160))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	if got := pixels.RGBAAt(60, 60); got != (color.RGBA{R: 0xed, G: 0xf8, B: 0xf1, A: 255}) {
		t.Fatalf("缩略图纸张颜色错误: %v", got)
	}
	if _, ok := state.images.Get(0); !ok || state.next != -1 {
		t.Fatal("改变底色不应清除或重新生成缩略图")
	}
}
