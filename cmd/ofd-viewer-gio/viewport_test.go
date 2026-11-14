package main

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestViewportFitPageAndWidth(t *testing.T) {
	var v pageViewport
	v.resize(image.Pt(800, 600), f32.Pt(210, 297))
	if v.scale != float32(600)/297 || v.offset.Y != 0 {
		t.Fatalf("适应页面错误: %+v", v)
	}
	v.fit(fitWidth)
	if v.scale != float32(800)/210 || v.offset != (f32.Point{}) {
		t.Fatalf("适应宽度未从页顶开始: %+v", v)
	}
	v.pan(f32.Pt(-20000, -20000))
	if v.offset.X != 0 || v.offset.Y != 600-297*v.scale {
		t.Fatalf("平移越界: %+v", v)
	}
	v.fit(fitWhole)
	if v.offset.Y != 0 || v.dragging {
		t.Fatalf("适应页面没有复位: %+v", v)
	}
}

func TestViewportZoomPreservesPointerAnchorAndLimitsScale(t *testing.T) {
	v := pageViewport{mode: fitManual, scale: 4, offset: f32.Pt(-100, -200)}
	v.resize(image.Pt(800, 600), f32.Pt(400, 400))
	anchor := f32.Pt(300, 200)
	before := anchor.Sub(v.offset).Div(v.scale)
	v.zoom(1.5, anchor, 1)
	after := anchor.Sub(v.offset).Div(v.scale)
	if before != after {
		t.Fatalf("指针下页面点发生移动: %v → %v", before, after)
	}
	v.zoom(1e6, anchor, 1)
	if v.scale != float32(96.0/25.4)*8 {
		t.Fatalf("超过最大缩放: %v", v.scale)
	}
	v.zoom(1e-8, anchor, 1)
	if math.Abs(float64(v.scale-float32(96.0/25.4)*0.1)) > 1e-6 {
		t.Fatalf("超过最小缩放: %v", v.scale)
	}
}

func TestViewportDragScrollAndCancel(t *testing.T) {
	var v pageViewport
	v.resize(image.Pt(800, 600), f32.Pt(210, 297))
	v.fit(fitWidth)
	v.handle(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(100, 200), PointerID: 1}, 1)
	v.handle(pointer.Event{Kind: pointer.Drag, Position: f32.Pt(100, 100), PointerID: 2}, 1)
	if v.offset.Y != 0 {
		t.Fatal("无关指针拖动了页面")
	}
	v.handle(pointer.Event{Kind: pointer.Drag, Position: f32.Pt(100, 100), PointerID: 1}, 1)
	if v.offset.Y != -100 {
		t.Fatalf("拖动偏移 = %v", v.offset)
	}
	v.handle(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, 50)}, 1)
	if v.offset.Y != -150 {
		t.Fatalf("滚轮平移偏移 = %v", v.offset)
	}
	v.resize(v.viewport, v.page)
	if v.offset.Y != -150 {
		t.Fatal("下一帧布局重置了适应宽度模式的滚动位置")
	}
	scale := v.scale
	v.handle(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, -100), Modifiers: key.ModCtrl, Position: f32.Pt(200, 200)}, 1)
	if v.scale <= scale || v.mode != fitManual {
		t.Fatal("Ctrl 滚轮未缩放")
	}
	v.handle(pointer.Event{Kind: pointer.Cancel}, 1)
	if v.dragging {
		t.Fatal("取消后仍处于拖动状态")
	}
}

type countedScene struct {
	*canvas.Canvas

	replays int
}

func TestManualViewportResizePreservesCenterAndFitResets(t *testing.T) {
	v := pageViewport{mode: fitManual, scale: 4, viewport: image.Pt(800, 600), page: f32.Pt(400, 400), offset: f32.Pt(-400, -500)}
	before := f32.Pt(400, 300).Sub(v.offset).Div(v.scale)
	v.resize(image.Pt(1000, 800), v.page)
	after := f32.Pt(500, 400).Sub(v.offset).Div(v.scale)
	if before != after || v.scale != 4 {
		t.Fatalf("手动模式改变窗口后中心漂移: %v → %v", before, after)
	}
	v.fit(fitWhole)
	if v.scale != 2 || v.offset != f32.Pt(100, 0) {
		t.Fatalf("恢复适应页面失败: %+v", v)
	}
}

func (s *countedScene) Width() float64                    { return s.W }
func (s *countedScene) Height() float64                   { return s.H }
func (s *countedScene) RenderTo(renderer canvas.Renderer) { s.replays++; s.Canvas.RenderTo(renderer) }

func TestViewportReusesRecordingAcrossZoomPanAndResize(t *testing.T) {
	scene := &countedScene{Canvas: canvas.New(210, 297)}
	v := viewer{scene: scene}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutViewport(gtx)
	for range 10 {
		ops.Reset()
		v.viewport.zoom(1.1, f32.Pt(400, 300), 1)
		v.viewport.pan(f32.Pt(-10, -20))
		v.layoutViewport(gtx)
	}
	gtx.Constraints = layout.Exact(image.Pt(900, 700))
	ops.Reset()
	v.layoutViewport(gtx)
	if scene.replays != 1 {
		t.Fatalf("缩放平移重复转换页面 %d 次", scene.replays)
	}
	v.recording.clear()
	ops.Reset()
	v.layoutViewport(gtx)
	if scene.replays != 2 {
		t.Fatal("清除缓存后没有重建记录")
	}
}

// 验证实际 Gio 事件路由，防止仅状态函数正确但阅读区收不到滚轮事件。
func TestViewportReceivesCtrlWheelFromGioRouter(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutViewport(gtx)
	router.Frame(&ops)
	scale := v.viewport.scale
	router.Queue(pointer.Event{Kind: pointer.Scroll, Position: f32.Pt(400, 300), Scroll: f32.Pt(0, -100), Modifiers: key.ModCtrl})
	ops.Reset()
	v.layoutViewport(gtx)
	if v.viewport.scale <= scale {
		t.Fatal("阅读区没有收到 Ctrl 滚轮事件")
	}
}

func TestScrollAtPageEdgeQueuesAdjacentPage(t *testing.T) {
	v := viewer{page: 1, total: 3, viewport: pageViewport{mode: fitManual, scale: 1, viewport: image.Pt(100, 100), page: f32.Pt(100, 300)}}
	scroll := func(y float32) { v.handleViewportPointer(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, y)}, 1) }
	scroll(190)
	if v.viewport.offset.Y != -190 || v.scrollPage != 0 {
		t.Fatal("页面内滚动不应翻页")
	}
	scroll(30)
	if v.viewport.offset.Y != -200 || v.scrollPage != 0 {
		t.Fatal("不足阈值的页边滚动不应翻页")
	}
	scroll(20)
	if v.scrollPage != 1 {
		t.Fatal("页底累计滚动应请求下一页")
	}
	v.scrollPage = 0
	v.viewport.offset.Y = 0
	scroll(-40)
	if v.scrollPage != -1 {
		t.Fatal("页顶向上滚动应请求上一页")
	}
	v.scrollPage, v.page = 0, 0
	scroll(-100)
	if v.scrollPage != 0 {
		t.Fatal("首页不能继续向前翻页")
	}
	v.page = 2
	v.viewport.offset.Y = -200
	scroll(100)
	if v.scrollPage != 0 {
		t.Fatal("末页不能继续向后翻页")
	}
}

func TestViewportReceivesBoundaryScrollFromGioRouter(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(40, 40)}, total: 3}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(160, 160)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutViewport(gtx)
	router.Frame(&ops)
	router.Queue(pointer.Event{Kind: pointer.Scroll, Position: f32.Pt(80, 80), Scroll: f32.Pt(0, 50)})
	ops.Reset()
	v.layoutViewport(gtx)
	if v.scrollPage != 1 {
		t.Fatal("阅读区未通过 Gio 路由接收滚轮跨页")
	}
}

func TestScrollPageBoundaryIgnoresZoomAndResetsOnDirectionChange(t *testing.T) {
	v := viewer{page: 1, total: 3, viewport: pageViewport{mode: fitManual, scale: 1, viewport: image.Pt(100, 100), page: f32.Pt(100, 100)}}
	v.handleViewportPointer(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, 30)}, 1)
	v.handleViewportPointer(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, -30)}, 1)
	if v.scrollPage != 0 || v.scrollOverflow != -30 {
		t.Fatal("改变方向应丢弃之前的页边滚动累计")
	}
	v.handleViewportPointer(pointer.Event{Kind: pointer.Scroll, Modifiers: key.ModCtrl, Scroll: f32.Pt(0, 200)}, 1)
	if v.scrollPage != 0 || v.scrollOverflow != 0 {
		t.Fatal("Ctrl 滚轮缩放不应触发跨页")
	}
}

func TestScrollLandingUsesNewPageDimensions(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(40, 80)}, landOnPage: -1}
	v.viewport.mode, v.viewport.scale = fitManual, 2
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(100, 100))}
	v.layoutViewport(gtx)
	if v.viewport.offset.Y != -60 || v.landOnPage != 0 {
		t.Fatalf("向上翻页应到新页面底部: offset=%v pending=%d", v.viewport.offset, v.landOnPage)
	}
}

func TestNativeGradientRecordingRebuildsOnlyForZoom(t *testing.T) {
	scene := &countedScene{Canvas: canvas.New(40, 40)}
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 40})}
	scene.RenderPath(canvas.Rectangle(40, 40), style, canvas.Identity)
	v := viewer{scene: scene}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(160, 160)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutViewport(gtx)
	if !v.recording.nativeLinear || scene.replays != 1 {
		t.Fatal("没有记录原生渐变")
	}
	for range 3 {
		ops.Reset()
		v.layoutViewport(gtx)
	}
	if scene.replays != 1 {
		t.Fatal("普通重绘重建了渐变记录")
	}
	v.viewport.zoom(2, f32.Point{}, 1)
	ops.Reset()
	v.layoutViewport(gtx)
	if scene.replays != 2 {
		t.Fatal("缩放后没有重建渐变色轴")
	}
	v.viewport.pan(f32.Pt(-10, -10))
	ops.Reset()
	v.layoutViewport(gtx)
	if scene.replays != 2 {
		t.Fatal("平移不应重建渐变记录")
	}
}

func TestMultistopGradientRecordingRebuildsForZoom(t *testing.T) {
	scene := &countedScene{Canvas: canvas.New(40, 40)}
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: (canvas.Grad{
		{Offset: 0, Color: color.RGBA{R: 128, A: 128}},
		{Offset: 0.5, Color: color.RGBA{G: 128, A: 128}},
		{Offset: 1, Color: color.RGBA{B: 128, A: 128}},
	}).ToLinear(canvas.Point{}, canvas.Point{X: 40})}
	scene.RenderPath(canvas.Rectangle(40, 40), style, canvas.Identity)
	v := viewer{scene: scene}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(160, 160)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutViewport(gtx)
	if !v.recording.nativeLinear {
		t.Fatal("多色标渐变没有进入原生路径")
	}
	v.viewport.zoom(2, f32.Pt(80, 80), 1)
	ops.Reset()
	v.layoutViewport(gtx)
	if scene.replays != 2 {
		t.Fatal("多色标渐变缩放后没有重建色轴")
	}
	for range 3 {
		ops.Reset()
		v.layoutViewport(gtx)
	}
	if scene.replays != 2 {
		t.Fatal("多色标渐变普通重绘没有复用操作记录")
	}
}

func TestNativeGradientViewportKeepsColorAxisAcrossZoomAndPan(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	scene := &countedScene{Canvas: canvas.New(40, 40)}
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: (canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}).ToLinear(canvas.Point{}, canvas.Point{X: 40})}
	scene.RenderPath(canvas.Rectangle(40, 40), style, canvas.Identity)
	v := viewer{scene: scene}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(160, 160)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	frame := func() *image.RGBA {
		ops.Reset()
		v.layoutViewport(gtx)
		if err := window.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, 160, 160))
		if err := window.Screenshot(pixels); err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	before := frame().RGBAAt(40, 80)
	// 原来的 x=40 像素中心所对应的页面点，缩放两倍后位于 80.5；
	// 再平移 -10 像素后在 70.5，应该保持同一中间色。
	v.viewport.zoom(2, f32.Point{}, 1)
	v.viewport.pan(f32.Pt(-10, -10))
	after := frame().RGBAAt(70, 80)
	for _, pair := range [][2]uint8{{before.R, after.R}, {before.B, after.B}} {
		if diff := int(pair[0]) - int(pair[1]); diff < -2 || diff > 2 {
			t.Fatalf("缩放平移后色轴漂移: before=%v after=%v", before, after)
		}
	}
	if before == (color.RGBA{R: 255, A: 255}) || before == (color.RGBA{B: 255, A: 255}) {
		t.Fatal("采样点不是中间色，未覆盖色轴漂移")
	}
}

func TestViewportReceivesDragAndReleaseOutsideReadingArea(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}}
	v.viewport.mode = fitWidth
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	frame := func() { ops.Reset(); v.layoutViewport(gtx); router.Frame(&ops) }
	frame()
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(400, 400)})
	frame()
	router.Queue(pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: f32.Pt(400, 300)})
	frame()
	if v.viewport.offset.Y != -100 {
		t.Fatalf("Gio 拖动未平移页面: %v", v.viewport.offset)
	}
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(1000, 900)})
	frame()
	if v.viewport.dragging {
		t.Fatal("阅读区外释放鼠标后仍处于拖动状态")
	}
}

func BenchmarkViewportCachedZoom(b *testing.B) {
	scene := &countedScene{Canvas: canvas.New(210, 297)}
	ctx := canvas.NewContext(scene.Canvas)
	for i := range 1000 {
		ctx.DrawPath(float64(i%20)*10, float64(i/20)*5, canvas.Rectangle(3, 3))
	}
	v := viewer{scene: scene}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(800, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.layoutViewport(gtx)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.Reset()
		factor := float32(1.01)
		if i%2 != 0 {
			factor = 1 / factor
		}
		v.viewport.zoom(factor, f32.Pt(400, 300), 1)
		v.layoutViewport(gtx)
	}
}
