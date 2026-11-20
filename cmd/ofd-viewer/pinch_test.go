package main

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPinchGestureResetsAndIgnoresOtherPointers(t *testing.T) {
	var g pinchGesture
	send := func(kind pointer.Kind, id pointer.ID, x float32) float32 {
		factor, _, _, _ := g.handle(pointer.Event{Kind: kind, Source: pointer.Touch, PointerID: id, Position: f32.Pt(x, 20)})
		return factor
	}
	send(pointer.Press, 1, 10)
	send(pointer.Press, 2, 30)
	send(pointer.Press, 3, 40)
	if factor := send(pointer.Drag, 3, 60); factor != 0 {
		t.Fatalf("第三根手指不应缩放: %v", factor)
	}
	if factor := send(pointer.Drag, 2, 50); factor != 2 {
		t.Fatalf("双指距离加倍应放大两倍: %v", factor)
	}
	send(pointer.Release, 2, 50)
	if factor := send(pointer.Drag, 1, 5); factor != 0 {
		t.Fatalf("抬起一根手指后不应缩放: %v", factor)
	}
	g.handle(pointer.Event{Kind: pointer.Cancel})
	if g.fingers[0].active || g.fingers[1].active {
		t.Fatal("取消后仍保留触点")
	}
}

func TestTouchPinchZoomsSingleAndContinuousViews(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(map[bool]string{false: "单页", true: "连续"}[continuous], func(t *testing.T) {
			v := viewer{viewport: pageViewport{mode: fitManual, scale: 4}, total: 2}
			if continuous {
				v.continuous.reset(0)
				v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{100, 100}}, {size: continuousPageSize{100, 100}}}
			} else {
				v.scene = &countedScene{Canvas: canvas.New(100, 100)}
			}
			var router input.Router
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			th := continuousTestTheme()
			frame := func() {
				ops.Reset()
				if continuous {
					v.layoutContinuous(gtx, th)
				} else {
					v.layoutViewport(gtx)
				}
				router.Frame(&ops)
			}
			frame()
			for _, e := range []pointer.Event{
				{Kind: pointer.Press, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(40, 60)},
				{Kind: pointer.Press, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(80, 60)},
				{Kind: pointer.Move, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(100, 60)},
			} {
				router.Queue(e)
				frame()
			}
			if v.viewport.scale < 5.9 || v.viewport.scale > 6.1 {
				t.Fatalf("双指张开后缩放比例错误: %v", v.viewport.scale)
			}
			router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Touch, PointerID: 2, Position: f32.Pt(70, 60)})
			frame()
			if v.viewport.scale < 2.9 || v.viewport.scale > 3.1 {
				t.Fatalf("双指捏合后缩放比例错误: %v", v.viewport.scale)
			}
		})
	}
}

func TestTouchDragPansZoomedViews(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(map[bool]string{false: "单页", true: "连续"}[continuous], func(t *testing.T) {
			v := viewer{viewport: pageViewport{mode: fitManual, scale: 4}, total: 2}
			if continuous {
				v.continuous.reset(0)
				v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{100, 100}}, {size: continuousPageSize{100, 100}}}
			} else {
				v.scene = &countedScene{Canvas: canvas.New(100, 100)}
			}
			var router input.Router
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			th := continuousTestTheme()
			frame := func() {
				ops.Reset()
				if continuous {
					v.layoutContinuous(gtx, th)
				} else {
					v.layoutViewport(gtx)
				}
				router.Frame(&ops)
			}
			frame()
			router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(80, 90)})
			frame()
			router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(50, 40)})
			frame()
			if continuous {
				if v.continuous.panX != -30 || v.continuous.list.Position.Offset != 50 {
					t.Fatalf("单指拖动未平移连续页面: x=%v y=%d", v.continuous.panX, v.continuous.list.Position.Offset)
				}
			} else if v.viewport.offset != f32.Pt(-30, -50) {
				t.Fatalf("单指拖动未平移页面: %v", v.viewport.offset)
			}
			router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(40, 30)})
			frame()
			if continuous {
				if v.continuous.panX != -40 || v.continuous.list.Position.Offset != 60 {
					t.Fatalf("持续拖动页面错误: x=%v y=%d", v.continuous.panX, v.continuous.list.Position.Offset)
				}
			} else if v.viewport.offset != f32.Pt(-40, -60) {
				t.Fatalf("持续拖动页面错误: %v", v.viewport.offset)
			}
			router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(40, 30)})
			frame()
			if v.viewport.dragging || v.continuous.dragging {
				t.Fatal("抬起手指后仍在拖动")
			}
		})
	}
}
