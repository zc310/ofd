package main

import (
	"image"
	"math"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	giocanvas "github.com/zc310/ofd/internal/thirdparty/canvas/renderers/gio"
)

type fitMode uint8

const (
	fitWhole fitMode = iota
	fitWidth
	fitManual
)

// pageViewport 的坐标均为阅读区局部像素；100% 对应 96 DPI 的逻辑尺寸。
type pageViewport struct {
	mode        fitMode
	scale       float32
	offset      f32.Point
	viewport    image.Point
	page        f32.Point
	dragging    bool
	pointerID   pointer.ID
	lastPointer f32.Point
	dragStart   f32.Point
	pinch       pinchGesture
	touch       touchGesture
}

func (v *pageViewport) fit(mode fitMode) {
	v.mode, v.offset, v.dragging = mode, f32.Point{}, false
	v.scale = 0
	v.resize(v.viewport, v.page)
}

func (v *pageViewport) resize(size image.Point, page f32.Point) {
	if size == v.viewport && page == v.page && v.scale > 0 {
		return
	}
	oldCenter := f32.Pt(float32(v.viewport.X)/2, float32(v.viewport.Y)/2)
	newCenter := f32.Pt(float32(size.X)/2, float32(size.Y)/2)
	v.viewport, v.page = size, page
	if size.X <= 0 || size.Y <= 0 || page.X <= 0 || page.Y <= 0 {
		return
	}
	if v.mode == fitManual && v.scale > 0 {
		v.offset = v.offset.Add(newCenter.Sub(oldCenter))
	} else {
		v.scale = float32(size.X) / page.X
		if v.mode == fitWhole {
			v.scale = min(v.scale, float32(size.Y)/page.Y)
		}
		v.offset = f32.Pt((float32(size.X)-page.X*v.scale)/2, 0)
		if v.mode == fitWhole {
			v.offset.Y = (float32(size.Y) - page.Y*v.scale) / 2
		}
	}
	v.clamp()
}

func (v *pageViewport) clamp() {
	clampAxis := func(offset, extent, available float32) float32 {
		if extent <= available {
			return (available - extent) / 2
		}
		return max(available-extent, min(0, offset))
	}
	v.offset.X = clampAxis(v.offset.X, v.page.X*v.scale, float32(v.viewport.X))
	v.offset.Y = clampAxis(v.offset.Y, v.page.Y*v.scale, float32(v.viewport.Y))
}

func (v *pageViewport) zoom(factor float32, anchor f32.Point, pixelsPerDp float32) {
	if v.scale <= 0 || factor <= 0 {
		return
	}
	base := float32(96.0/25.4) * max(pixelsPerDp, 1)
	next := max(base*0.1, min(base*8, v.scale*factor))
	v.offset = anchor.Sub(anchor.Sub(v.offset).Mul(next / v.scale))
	v.scale, v.mode = next, fitManual
	v.clamp()
}

func (v *pageViewport) pan(delta f32.Point) { v.offset = v.offset.Add(delta); v.clamp() }

func (v *pageViewport) handle(e pointer.Event, pixelsPerDp float32) {
	switch e.Kind { //nolint:exhaustive // 仅处理拖动和滚动相关事件。
	case pointer.Press:
		if e.Source == pointer.Touch || (e.Source == pointer.Mouse && e.Buttons&pointer.ButtonPrimary != 0) {
			v.dragging, v.pointerID, v.lastPointer = true, e.PointerID, e.Position
			v.dragStart = e.Position
		}
	case pointer.Drag:
		if v.dragging && e.PointerID == v.pointerID {
			v.pan(e.Position.Sub(v.lastPointer))
			v.lastPointer = e.Position
		}
	case pointer.Release:
		if e.PointerID == v.pointerID {
			v.dragging = false
		}
	case pointer.Cancel:
		v.dragging = false
	case pointer.Scroll:
		if e.Modifiers&key.ModCtrl != 0 {
			// 使用连续指数响应，兼顾鼠标滚轮和触控板的小步滚动。
			v.zoom(float32(math.Exp(float64(-e.Scroll.Y/max(pixelsPerDp, 1))/300)), e.Position, pixelsPerDp)
		} else {
			v.pan(e.Scroll.Mul(-1))
		}
	}
}

// sceneRecording 缓存当前页的 Gio 操作；普通页面缩放、拖动和重绘只添加变换。
// 含原生渐变的页面在缩放变化时重建，补偿 Gio 色轴未跟随外层缩放的问题；
// 平移和普通重绘仍复用。只保留一页，换页即释放引用。
type sceneRecording struct {
	scene        pageScene
	ops          op.Ops
	call         op.CallOp
	scale        float32
	nativeLinear bool
}

func (r *sceneRecording) clear() { *r = sceneRecording{} }

// drawCached 只重放已生成的 Gio 操作，不访问页面或文档资源。
// 含原生渐变的记录在比例变化时不能复用，需要等 worker 归还文档后重建。
func (r *sceneRecording) drawCached(gtx layout.Context, scene pageScene, scale float32) bool {
	if r.scene == nil || r.scene != scene || (r.nativeLinear && r.scale != scale) {
		return false
	}
	r.call.Add(gtx.Ops)
	return true
}

func (r *sceneRecording) draw(gtx layout.Context, scene pageScene, scale float32) {
	if r.scene != scene || (r.nativeLinear && r.scale != scale) {
		r.clear()
		m := op.Record(&r.ops)
		recordContext := gtx
		recordContext.Ops = &r.ops
		renderer := giocanvas.New(recordContext, scene.Width(), scene.Height())
		renderer.SetGradientScale(scale)
		scene.RenderTo(renderer)
		r.scale, r.nativeLinear = scale, renderer.HasNativeGradients()
		r.call, r.scene = m.Stop(), scene
	}
	r.call.Add(gtx.Ops)
}

func (v *viewer) layoutViewport(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()
	previousScale := v.viewport.scale
	v.viewport.resize(size, f32.Pt(float32(v.scene.Width()), float32(v.scene.Height())))
	v.applySingleLinkDestination(size, gtx.Metric.PxPerDp)
	if v.landOnPage != 0 {
		v.viewport.offset.Y = 0
		if v.landOnPage < 0 {
			v.viewport.offset.Y = float32(size.Y) - v.viewport.page.Y*v.viewport.scale
		}
		v.viewport.clamp()
		v.landOnPage = 0
	}
	if previousScale != v.viewport.scale {
		// 工具栏先于阅读区布局，补一帧同步显示本次自适应的比例。
		gtx.Execute(op.InvalidateCmd{})
	}
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &v.viewport,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1000000, Max: 1000000},
			ScrollY: pointer.ScrollRange{Min: -1000000, Max: 1000000}})
		if !ok {
			break
		}
		if e, ok := e.(pointer.Event); ok {
			factor, anchor, delta, grab := v.viewport.pinch.handle(e)
			if v.viewport.touch.handle(e, gtx.Metric.PxPerDp, grab || v.viewport.pinch.active()) {
				v.doubleTapZoom(e.Position, gtx.Metric.PxPerDp)
			}
			if grab {
				v.viewport.dragging = false
				for _, finger := range v.viewport.pinch.fingers {
					gtx.Execute(pointer.GrabCmd{Tag: &v.viewport, ID: finger.id})
				}
			}
			if factor > 0 {
				v.viewport.zoom(factor, anchor, gtx.Metric.PxPerDp)
				v.viewport.pan(delta)
			}
			if e.Source != pointer.Touch || !v.viewport.pinch.active() {
				before := v.viewport.offset.X
				move := e.Position.X - v.viewport.lastPointer.X
				v.handleViewportPointer(e, gtx.Metric.PxPerDp)
				v.handleTouchPageSwipe(e, before, move, gtx.Metric.PxPerDp)
			}
			gtx.Execute(op.InvalidateCmd{})
			if v.viewport.dragging && e.Kind == pointer.Drag && linkDragExceeded(e.Position.Sub(v.viewport.dragStart), gtx.Metric.PxPerDp) {
				gtx.Execute(pointer.GrabCmd{Tag: &v.viewport, ID: v.viewport.pointerID})
			}
		}
	}
	event.Op(gtx.Ops, &v.viewport)
	// 原生渐变当前仅自动处理整数平移；将显示偏移吸附到像素，
	// 保留逻辑偏移的精度供下一次缩放锚点计算使用。
	transform := op.Affine(f32.NewAffine2D(v.viewport.scale, 0, float32(math.Round(float64(v.viewport.offset.X))),
		0, v.viewport.scale, float32(math.Round(float64(v.viewport.offset.Y))))).Push(gtx.Ops)
	var paper clip.Path
	paper.Begin(gtx.Ops)
	paper.MoveTo(f32.Point{})
	paper.LineTo(f32.Pt(v.viewport.page.X, 0))
	paper.LineTo(v.viewport.page)
	paper.LineTo(f32.Pt(0, v.viewport.page.Y))
	paper.Close()
	paperClip := (clip.Outline{Path: paper.End()}).Op().Push(gtx.Ops)
	paint.Fill(gtx.Ops, paperColor(v.background, v.backgroundCustom))
	v.recording.draw(gtx, v.scene, v.viewport.scale)
	v.layoutPageLinks(gtx, &v.pageLinks, v.scene, v.viewport.scale, v.viewport.dragging)
	paperClip.Pop()
	transform.Pop()
	cursor := pointer.CursorGrab
	if v.viewport.dragging {
		cursor = pointer.CursorGrabbing
	}
	cursor.Add(gtx.Ops)
	return layout.Dimensions{Size: size}
}

// handleTouchPageSwipe 仅累计页面已到水平边缘后的未消费位移，避免拖动画面时误翻页。
func (v *viewer) handleTouchPageSwipe(e pointer.Event, before, move, pixelsPerDp float32) {
	g := &v.viewport.touch
	if e.Source != pointer.Touch || v.busy || v.scrollPage != 0 {
		return
	}
	switch e.Kind { //nolint:exhaustive // 仅处理单指拖动和释放。
	case pointer.Drag:
		if g.down && e.PointerID == g.id && v.viewport.dragging {
			remaining := move - (v.viewport.offset.X - before)
			if g.overflowX*remaining < 0 {
				g.overflowX = 0
			}
			g.overflowX += remaining
		}
	case pointer.Release:
		delta := e.Position.Sub(g.start)
		if g.moved && float32(math.Abs(float64(g.overflowX))) >= 60*max(1, pixelsPerDp) &&
			math.Abs(float64(delta.X)) > 1.2*math.Abs(float64(delta.Y)) {
			direction := 1
			if g.overflowX > 0 {
				direction = -1
			}
			if v.page+direction >= 0 && v.page+direction < v.total {
				v.scrollPage = direction
			}
		}
	}
}

// handleViewportPointer 将普通滚轮到达页边后未消费的距离累计为翻页意图；
// 请求延后到下一帧提交，避免后台更换文档时当前帧仍在构建页面操作。
func (v *viewer) handleViewportPointer(e pointer.Event, pixelsPerDp float32) {
	before := v.viewport.offset.Y
	v.viewport.handle(e, pixelsPerDp)
	if e.Kind != pointer.Scroll || e.Modifiers&key.ModCtrl != 0 || e.Scroll.Y == 0 || v.busy || v.scrollPage != 0 {
		v.scrollOverflow = 0
		return
	}
	remaining := e.Scroll.Y - (before - v.viewport.offset.Y)
	if math.Abs(float64(remaining)) < 0.01 || remaining*e.Scroll.Y <= 0 {
		v.scrollOverflow = 0
		return
	}
	direction := 1
	if remaining < 0 {
		direction = -1
	}
	if v.page+direction < 0 || v.page+direction >= v.total {
		v.scrollOverflow = 0
		return
	}
	if v.scrollOverflow*remaining < 0 {
		v.scrollOverflow = 0
	}
	v.scrollOverflow += remaining
	if math.Abs(float64(v.scrollOverflow)) >= float64(40*max(pixelsPerDp, 1)) {
		v.scrollPage = direction
		v.scrollOverflow = 0
	}
}
