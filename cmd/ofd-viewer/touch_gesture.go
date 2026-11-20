package main

import (
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// touchGesture 区分单指点击、拖动与多指操作；双击只接受相近位置和时间。
type touchGesture struct {
	id          pointer.ID
	down        bool
	moved       bool
	start       f32.Point
	startTime   time.Duration
	lastTap     f32.Point
	lastTapTime time.Duration
	hasLastTap  bool
	overflowX   float32
}

func (g *touchGesture) handle(e pointer.Event, pixelsPerDp float32, multi bool) bool {
	if e.Kind == pointer.Cancel || multi {
		*g = touchGesture{}
		return false
	}
	if e.Source != pointer.Touch {
		return false
	}
	switch e.Kind { //nolint:exhaustive // 只识别触摸操作。
	case pointer.Press:
		g.id, g.down, g.moved, g.start, g.startTime = e.PointerID, true, false, e.Position, e.Time
		g.overflowX = 0
	case pointer.Drag:
		if g.down && e.PointerID == g.id && linkDragExceeded(e.Position.Sub(g.start), pixelsPerDp) {
			g.moved = true
			g.hasLastTap = false
		}
	case pointer.Release:
		if !g.down || e.PointerID != g.id {
			return false
		}
		g.down = false
		elapsed := e.Time - g.startTime
		if g.moved || linkDragExceeded(e.Position.Sub(g.start), pixelsPerDp) || elapsed < 0 || elapsed > 350*time.Millisecond {
			g.hasLastTap = false
			return false
		}
		interval := e.Time - g.lastTapTime
		distance := e.Position.Sub(g.lastTap)
		if g.hasLastTap && interval >= 0 && interval <= 350*time.Millisecond &&
			math.Hypot(float64(distance.X), float64(distance.Y)) <= float64(24*max(1, pixelsPerDp)) {
			g.hasLastTap = false
			return true
		}
		g.lastTap, g.lastTapTime, g.hasLastTap = e.Position, e.Time, true
	}
	return false
}

// doubleTapZoom 在适合宽度和以点击位置为中心的两倍宽度之间切换。
func (v *viewer) doubleTapZoom(anchor f32.Point, pixelsPerDp float32) {
	width := float32(v.viewport.viewport.X)
	if v.continuous.enabled {
		width -= 24 * max(1, pixelsPerDp)
	}
	if v.viewport.page.X <= 0 || v.viewport.scale <= 0 || width <= 0 {
		return
	}
	fitScale := width / v.viewport.page.X
	if v.viewport.mode == fitManual && v.viewport.scale > fitScale*1.2 {
		v.viewport.fit(fitWidth)
		if v.continuous.enabled {
			v.continuous.panX = 0
		}
		return
	}
	v.zoomViewport(2*fitScale/v.viewport.scale, anchor, pixelsPerDp)
}
