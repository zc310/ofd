package main

import (
	"context"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func continuousTestTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	return th
}

func BenchmarkContinuousViewZoom(b *testing.B) {
	for _, tc := range []struct {
		name     string
		gradient bool
		pinch    bool
	}{{"普通页面", false, false}, {"含渐变页面", true, false}, {"含渐变页面双指预览", true, true}} {
		b.Run(tc.name, func(b *testing.B) {
			scene := &countedScene{Canvas: canvas.New(210, 297)}
			ctx := canvas.NewContext(scene.Canvas)
			for i := range 1000 {
				ctx.DrawPath(float64(i%20)*10, float64(i/20)*5, canvas.Rectangle(3, 3))
			}
			if tc.gradient {
				style := canvas.DefaultStyle
				style.Fill = canvas.Paint{Gradient: (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 40})}
				scene.RenderPath(canvas.Rectangle(40, 40), style, canvas.Identity)
			}
			v := viewer{viewport: pageViewport{mode: fitManual, scale: 2}, total: 1}
			v.continuous.enabled = true
			v.continuous.reset(0)
			v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{210, 297}}}
			v.continuous.entries.AddWeighted(0, &continuousEntry{scene: scene}, 128)
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			th := continuousTestTheme()
			v.layoutContinuous(gtx, th)
			if tc.pinch {
				v.continuous.pinch.fingers[0].active = true
				v.continuous.pinch.fingers[1].active = true
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				factor := float32(1.01)
				if i%2 != 0 {
					factor = 1 / factor
				}
				v.continuous.zoom(&v.viewport, factor, f32.Pt(400, 300), 1)
				ops.Reset()
				v.layoutContinuous(gtx, th)
			}
		})
	}
}

func TestContinuousPinchReusesGradientRecordingUntilRelease(t *testing.T) {
	scene := &countedScene{Canvas: canvas.New(100, 100)}
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 40})}
	scene.RenderPath(canvas.Rectangle(40, 40), style, canvas.Identity)
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 2}, total: 1}
	v.continuous.enabled = true
	v.continuous.reset(0)
	v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{100, 100}}}
	entry := &continuousEntry{scene: scene}
	v.continuous.entries.AddWeighted(0, entry, 128)
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	frame := func() { ops.Reset(); v.layoutContinuous(gtx, th); router.Frame(&ops) }
	frame()
	if scene.replays != 1 || !entry.recording.nativeLinear {
		t.Fatal("初始页面应建立渐变记录")
	}
	for _, e := range []pointer.Event{
		{Kind: pointer.Press, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(40, 60)},
		{Kind: pointer.Press, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(80, 60)},
		{Kind: pointer.Move, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(100, 60)},
		{Kind: pointer.Move, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(90, 60)},
	} {
		router.Queue(e)
		frame()
	}
	if scene.replays != 1 || entry.recording.scale == v.viewport.scale {
		t.Fatalf("捏合期间应复用原记录: replays=%d 记录比例=%v 当前比例=%v", scene.replays, entry.recording.scale, v.viewport.scale)
	}
	router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(90, 60)})
	frame()
	if scene.replays != 2 || entry.recording.scale != v.viewport.scale {
		t.Fatalf("松手后应重建精确渐变: replays=%d 记录比例=%v 当前比例=%v", scene.replays, entry.recording.scale, v.viewport.scale)
	}
}

func TestContinuousViewDisplaysTwoPagesOnGPU(t *testing.T) {
	window, err := headless.NewWindow(120, 140)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 0.5}, total: 2}
	v.continuous.reset(0)
	for page := range 2 {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
		v.continuous.entries.AddWeighted(page, &continuousEntry{scene: &countedScene{Canvas: canvas.New(100, 100)}}, 128)
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutContinuous(gtx, continuousTestTheme())
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 120, 140))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{image.Pt(60, 20), image.Pt(60, 82)} {
		if c := pixels.RGBAAt(p.X, p.Y); c.R < 245 || c.G < 245 || c.B < 245 || c.A != 255 {
			t.Fatalf("连续页面 %v 未显示: %v", p, c)
		}
	}
}

func TestDoublePagePairsUnequalSizesAndPreservesSelectedPage(t *testing.T) {
	window, err := headless.NewWindow(240, 230)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 1}, total: 3, page: 1}
	v.continuous.double = true
	v.continuous.reset(1)
	v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{80, 100}}, {size: continuousPageSize{100, 60}}, {size: continuousPageSize{60, 80}}}
	for page, meta := range v.continuous.metadata {
		v.continuous.entries.AddWeighted(page, &continuousEntry{scene: &countedScene{Canvas: canvas.New(meta.size.width, meta.size.height)}}, 128)
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(240, 230)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutContinuous(gtx, continuousTestTheme())
	if v.page != 1 {
		t.Fatalf("同行第二页选择被覆盖: %d", v.page)
	}
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 240, 230))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{image.Pt(40, 30), image.Pt(150, 30), image.Pt(120, 130)} {
		if c := pixels.RGBAAt(p.X, p.Y); c.R < 245 || c.G < 245 || c.B < 245 || c.A != 255 {
			t.Fatalf("双页页面 %v 未显示: %v", p, c)
		}
	}
	if c := pixels.RGBAAt(150, 90); c.A != 0 {
		t.Fatalf("较矮页面下方不应绘制纸张: %v", c)
	}
	v.continuous.jump(2)
	if v.continuous.list.Position.First != 1 {
		t.Fatal("奇数末页应跳至第二行")
	}
}

func TestDoublePageToggleKeepsGlobalPageAndSettings(t *testing.T) {
	v := viewer{page: 3, scene: &countedScene{Canvas: canvas.New(100, 100)}}
	var settings viewerSettings
	saved := 0
	v.doublePageButton.Click()
	v.handleViewMenu(layout.Context{}, &settings, func() { saved++ }, func(request) {})
	if !v.continuous.enabled || !v.continuous.double || !settings.Continuous || !settings.DoublePage || v.continuous.list.Position.First != 1 || v.page != 3 || saved != 1 {
		t.Fatalf("双页开启状态错误: %+v", settings)
	}
	v.doublePageButton.Click()
	v.handleViewMenu(layout.Context{}, &settings, func() { saved++ }, func(request) {})
	if !v.continuous.enabled || v.continuous.double || settings.DoublePage || v.continuous.list.Position.First != 3 || v.page != 3 {
		t.Fatal("关闭双页应保持连续阅读和当前页")
	}
}

func TestDoublePageScrollRequestsVisiblePagesAndSynchronizesRow(t *testing.T) {
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 1}, total: 7}
	v.continuous.double = true
	v.continuous.reset(0)
	for range v.total {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
	}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(240, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	v.layoutContinuous(gtx, th)
	if len(v.continuous.next) < 2 || v.continuous.next[0] != 0 || v.continuous.next[1] != 1 {
		t.Fatalf("双页应请求左右两页: %v", v.continuous.next)
	}
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Scroll, Position: f32.Pt(100, 70), Scroll: f32.Pt(0, 250)})
	ops.Reset()
	v.layoutContinuous(gtx, th)
	if v.page != 4 || v.pageInput.Text() != "5" || v.thumbnails.list.Position.First != 4 {
		t.Fatalf("双页滚动后全局页码错误: %d %q", v.page, v.pageInput.Text())
	}
	if len(v.continuous.next) > pageSceneCacheEntries {
		t.Fatalf("可见页超过缓存预算: %v", v.continuous.next)
	}
}

func TestContinuousViewKeepsRecordedContentWhileWorkerBusy(t *testing.T) {
	window, err := headless.NewWindow(120, 140)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	scene := &countedScene{Canvas: canvas.New(100, 100)}
	canvas.NewContext(scene.Canvas).DrawPath(30, 30, canvas.Rectangle(40, 40))
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 0.5}, total: 1}
	v.continuous.reset(0)
	v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{100, 100}}}
	v.continuous.entries.AddWeighted(0, &continuousEntry{scene: scene}, 128)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	v.layoutContinuous(gtx, th)
	if scene.replays != 1 {
		t.Fatalf("初次显示未建立记录: %d", scene.replays)
	}
	v.busy = true
	ops.Reset()
	v.layoutContinuous(gtx, th)
	if scene.replays != 1 {
		t.Fatal("worker 忙时不能重放页面记录")
	}
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 120, 140))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	if c := pixels.RGBAAt(60, 25); c.R > 20 || c.G > 20 || c.B > 20 || c.A != 255 {
		t.Fatalf("加载期间未保留已记录内容: %v", c)
	}
}

func TestCachedRecordingRejectsChangedNativeGradientScale(t *testing.T) {
	scene := &countedScene{Canvas: canvas.New(100, 100)}
	r := sceneRecording{scene: scene, nativeLinear: true, scale: 1}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops}
	if r.drawCached(gtx, scene, 2) {
		t.Fatal("缩放变化时不能复用原生渐变操作")
	}
}

func TestContinuousViewScrollsAndSynchronizesPage(t *testing.T) {
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 1}, total: 20}
	v.continuous.reset(0)
	for range v.total {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
	}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	v.layoutContinuous(gtx, th)
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Scroll, Position: f32.Pt(60, 70), Scroll: f32.Pt(0, 250)})
	ops.Reset()
	v.layoutContinuous(gtx, th)
	if v.page == 0 || v.pageInput.Text() != "3" || v.thumbnails.list.Position.First != v.page {
		t.Fatalf("滚动后页码未同步: page=%d input=%q", v.page, v.pageInput.Text())
	}
	if len(v.continuous.next) == 0 || len(v.continuous.next) > pageSceneCacheEntries {
		t.Fatalf("可见页请求无效: %v", v.continuous.next)
	}
}

func TestContinuousViewCtrlWheelZoomsWithoutScrolling(t *testing.T) {
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 1}, total: 20}
	v.continuous.reset(0)
	for range v.total {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
	}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	v.layoutContinuous(gtx, th)
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Scroll, Position: f32.Pt(60, 40), Scroll: f32.Pt(0, -100), Modifiers: key.ModCtrl})
	ops.Reset()
	v.layoutContinuous(gtx, th)
	if v.viewport.scale <= 1 || v.page != 0 {
		t.Fatalf("Ctrl 滚轮应缩放而不是翻页: scale=%v page=%d", v.viewport.scale, v.page)
	}
	if offset := v.continuous.list.Position.Offset; offset < 10 || offset > 25 {
		t.Fatalf("缩放未保持首个页面的鼠标锚点: offset=%d", offset)
	}
}

func TestContinuousViewMouseDragPansBothAxes(t *testing.T) {
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 2}, total: 20}
	v.continuous.reset(0)
	for range v.total {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
	}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	frame := func() { ops.Reset(); v.layoutContinuous(gtx, th); router.Frame(&ops) }
	frame()
	router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(80, 90), PointerID: 1})
	frame()
	router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(50, 40), PointerID: 1})
	frame()
	if v.continuous.panX != -30 || v.continuous.list.Position.Offset != 50 {
		t.Fatalf("连续视口拖动错误: x=%v y=%d", v.continuous.panX, v.continuous.list.Position.Offset)
	}
	router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(1000, 900), PointerID: 1})
	frame()
	if v.continuous.dragging {
		t.Fatal("释放鼠标后仍在拖动")
	}
}

func TestContinuousPanClampsAndAccumulatesFractionalScroll(t *testing.T) {
	c := continuousView{contentWidth: 200}
	c.panHorizontal(-1000, 120)
	if c.panX != -80 {
		t.Fatalf("水平平移应限制在页边: %v", c.panX)
	}
	c.panHorizontal(1000, 120)
	if c.panX != 0 {
		t.Fatalf("水平平移应限制在页边: %v", c.panX)
	}
	for range 4 {
		c.scrollVertical(0.3)
	}
	if c.list.Position.Offset != 1 {
		t.Fatalf("触控板的小步滚动没有累积: %d", c.list.Position.Offset)
	}
}

func TestContinuousViewKeepsScrollbarInteractive(t *testing.T) {
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 1}, total: 20}
	v.continuous.reset(0)
	for range v.total {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
	}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	frame := func() { ops.Reset(); v.layoutContinuous(gtx, th); router.Frame(&ops) }
	frame()
	router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(115, 120)})
	frame()
	router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(115, 120)})
	frame()
	if v.page == 0 {
		t.Fatal("阅读区手势覆盖了滚动条")
	}
}

func TestContinuousViewDoesNotThrashWhenVisiblePagesExceedBudget(t *testing.T) {
	var c continuousView
	c.reset(0)
	c.visible = []int{0, 1}
	c.pending = continuousPagesRequest
	large := func() *batchWeightedScene {
		return &batchWeightedScene{Canvas: canvas.New(10, 10), weight: pageSceneCacheBytes/2 + 1}
	}
	c.accept(result{pages: []pageResult{{page: 0, scene: large()}, {page: 1, scene: large()}}})
	if _, ok := c.entries.Get(0); !ok || !c.blocked[1] || c.entries.Weight() > pageSceneCacheBytes {
		t.Fatal("超出预算的可见页面不应反复淘汰其他可见页")
	}
}

func TestContinuousViewLoadsVisiblePagesThroughWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	requests, results := make(chan request, 1), make(chan result, 1)
	done := make(chan struct{})
	go func() { defer close(done); runWorker(ctx, requests, results, func() {}, nil) }()
	v := viewer{viewport: pageViewport{mode: fitManual, scale: 1}}
	v.continuous.enabled = true
	defer func() { v.sceneLease.release(); cancel(); <-done }()
	requests <- request{filename: createTestDocument(t)}
	v.pendingOpen = true
	v.acceptResponse(<-results, func() {}, func(string) {})
	submit := func(req request) {
		v.busy, v.scene = true, nil
		requests <- req
	}
	accept := func() { v.acceptWorkerResponse(<-results, func() {}, func(string) {}) }
	v.scheduleContinuous(submit)
	if !v.busy || v.continuous.pending != continuousMetadataRequest {
		t.Fatal("连续视口没有请求元数据")
	}
	accept()
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(250, 700)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	v.layoutContinuous(gtx, th)
	v.scheduleContinuous(submit)
	if !v.busy || v.continuous.pending != continuousPagesRequest {
		t.Fatalf("连续视口没有请求可见页: %v", v.continuous.next)
	}
	accept()
	ops.Reset()
	v.layoutContinuous(gtx, th)
	if v.continuous.entries.Len() != 2 || len(v.continuous.next) != 0 {
		t.Fatalf("连续页面未载入: entries=%d next=%v", v.continuous.entries.Len(), v.continuous.next)
	}
	for page := range 2 {
		entry, ok := v.continuous.entries.Get(page)
		if !ok || entry.recording.scene != entry.scene {
			t.Fatalf("第 %d 页没有重放", page+1)
		}
	}
}

func TestContinuousViewDefersPageLoadingUntilPinchEnds(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(100, 100)}, viewport: pageViewport{mode: fitManual, scale: 1}, total: 2}
	v.continuous.enabled = true
	v.continuous.reset(0)
	v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{100, 100}}, {size: continuousPageSize{100, 100}}}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	frame := func() { ops.Reset(); v.layoutContinuous(gtx, th); router.Frame(&ops) }
	frame()
	for id, x := range []float32{40, 80} {
		router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Touch, PointerID: pointer.ID(id + 1), Position: f32.Pt(x, 60)})
		frame()
		var requested bool
		v.scheduleContinuous(func(request) { requested = true })
		if requested {
			t.Fatal("触摸期间不应启动页面加载")
		}
	}
	var requested []request
	submit := func(req request) { requested = append(requested, req) }
	v.scheduleContinuous(submit)
	if len(requested) != 0 || v.continuous.pending != 0 {
		t.Fatal("双指缩放期间不应启动页面加载")
	}
	router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(80, 60)})
	frame()
	v.scheduleContinuous(submit)
	if len(requested) != 0 {
		t.Fatal("还有手指按住时不应启动页面加载")
	}
	router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(40, 60)})
	frame()
	v.scheduleContinuous(submit)
	if len(requested) != 1 || len(requested[0].pages) == 0 || v.continuous.pending != continuousPagesRequest {
		t.Fatalf("手势结束后应加载可见页面: %+v", requested)
	}
}
