package main

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestDoubleTapZoomsAndRestoresFitWidth(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(map[bool]string{false: "单页", true: "连续"}[continuous], func(t *testing.T) {
			v := viewer{viewport: pageViewport{mode: fitWidth}, total: 2}
			if continuous {
				v.continuous.reset(0)
				v.continuous.enabled = true
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
			initial := v.viewport.scale
			tap := func(at time.Duration) {
				router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(45, 55), Time: at})
				frame()
				router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(45, 55), Time: at + 30*time.Millisecond})
				frame()
			}
			tap(time.Second)
			tap(time.Second + 150*time.Millisecond)
			if v.viewport.scale < initial*1.99 || v.viewport.mode != fitManual {
				t.Fatalf("双击未放大: %v → %v", initial, v.viewport.scale)
			}
			tap(2 * time.Second)
			tap(2*time.Second + 150*time.Millisecond)
			if v.viewport.mode != fitWidth || v.viewport.scale < initial*0.99 || v.viewport.scale > initial*1.01 {
				t.Fatalf("双击未恢复适合宽度: mode=%v scale=%v 初始=%v", v.viewport.mode, v.viewport.scale, initial)
			}
		})
	}
}

func TestTouchSwipeTurnsPageOnlyAfterHorizontalEdge(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(100, 100)}, viewport: pageViewport{mode: fitManual, scale: 4}, page: 1, total: 3}
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(120, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	frame := func() { ops.Reset(); v.layoutViewport(gtx); router.Frame(&ops) }
	frame()
	send := func(kind pointer.Kind, x, y float32) {
		router.Queue(pointer.Event{Kind: kind, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(x, y)})
		frame()
	}
	send(pointer.Press, 100, 70)
	send(pointer.Move, 20, 70)
	send(pointer.Release, 20, 70)
	if v.scrollPage != 0 || v.viewport.offset.X != -80 {
		t.Fatalf("未到页边的拖动不应翻页: page=%d offset=%v", v.scrollPage, v.viewport.offset.X)
	}
	v.viewport.offset.X = -280
	send(pointer.Press, 100, 70)
	send(pointer.Move, 20, 70)
	send(pointer.Release, 20, 70)
	if v.scrollPage != 1 {
		t.Fatalf("向左滑动页边应翻到下一页: %d", v.scrollPage)
	}
	v.scrollPage = 0
	v.viewport.offset.X = 0
	send(pointer.Press, 20, 70)
	send(pointer.Move, 100, 70)
	send(pointer.Release, 100, 70)
	if v.scrollPage != -1 {
		t.Fatalf("向右滑动页边应翻到上一页: %d", v.scrollPage)
	}
	v.scrollPage = 0
	v.page = 0
	send(pointer.Press, 20, 70)
	send(pointer.Move, 100, 70)
	send(pointer.Release, 100, 70)
	if v.scrollPage != 0 {
		t.Fatal("首页不能继续向前翻页")
	}
	v.page = 1
	send(pointer.Press, 20, 120)
	send(pointer.Move, 100, 10)
	send(pointer.Release, 100, 10)
	if v.scrollPage != 0 {
		t.Fatal("纵向为主的拖动不能触发翻页")
	}
}

func TestDoubleTapRejectsDragAndMultitouch(t *testing.T) {
	var g touchGesture
	send := func(kind pointer.Kind, x float32, at time.Duration, multi bool) bool {
		return g.handle(pointer.Event{Kind: kind, Source: pointer.Touch, PointerID: 1, Position: f32.Pt(x, 20), Time: at}, 1, multi)
	}
	send(pointer.Press, 20, 0, false)
	send(pointer.Release, 20, 10*time.Millisecond, false)
	send(pointer.Press, 20, 100*time.Millisecond, false)
	send(pointer.Drag, 40, 110*time.Millisecond, false)
	if send(pointer.Release, 20, 120*time.Millisecond, false) {
		t.Fatal("拖动后返回原位不能算双击")
	}
	send(pointer.Press, 20, 200*time.Millisecond, false)
	send(pointer.Release, 20, 210*time.Millisecond, false)
	send(pointer.Press, 20, 250*time.Millisecond, true)
	if send(pointer.Release, 20, 260*time.Millisecond, false) {
		t.Fatal("多指操作不能算双击")
	}
}
