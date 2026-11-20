package main

import (
	"math"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

type pinchFinger struct {
	id       pointer.ID
	position f32.Point
	active   bool
}

// pinchGesture 只跟踪最先按下的两根手指；取消或抬起后不沿用旧距离。
type pinchGesture struct {
	fingers [2]pinchFinger
}

func (g *pinchGesture) active() bool {
	return g.fingers[0].active && g.fingers[1].active
}

func (g *pinchGesture) handle(e pointer.Event) (float32, f32.Point, f32.Point, bool) {
	if e.Kind == pointer.Cancel {
		*g = pinchGesture{}
		return 0, f32.Point{}, f32.Point{}, false
	}
	if e.Source != pointer.Touch {
		return 0, f32.Point{}, f32.Point{}, false
	}
	switch e.Kind { //nolint:exhaustive // 只处理触摸按下、移动和释放。
	case pointer.Press:
		for _, finger := range g.fingers {
			if finger.active && finger.id == e.PointerID {
				return 0, f32.Point{}, f32.Point{}, false
			}
		}
		for i := range g.fingers {
			if !g.fingers[i].active {
				g.fingers[i] = pinchFinger{id: e.PointerID, position: e.Position, active: true}
				return 0, f32.Point{}, f32.Point{}, g.fingers[0].active && g.fingers[1].active
			}
		}
	case pointer.Drag:
		for i := range g.fingers {
			if !g.fingers[i].active || g.fingers[i].id != e.PointerID {
				continue
			}
			oldA, oldB := g.fingers[0].position, g.fingers[1].position
			g.fingers[i].position = e.Position
			if !g.fingers[0].active || !g.fingers[1].active {
				return 0, f32.Point{}, f32.Point{}, false
			}
			a, b := g.fingers[0].position, g.fingers[1].position
			oldDistance := math.Hypot(float64(oldA.X-oldB.X), float64(oldA.Y-oldB.Y))
			newDistance := math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y))
			if oldDistance < 1 || newDistance < 1 {
				return 0, f32.Point{}, f32.Point{}, false
			}
			oldCenter := oldA.Add(oldB).Mul(0.5)
			anchor := a.Add(b).Mul(0.5)
			return float32(newDistance / oldDistance), oldCenter, anchor.Sub(oldCenter), false
		}
	case pointer.Release:
		for i := range g.fingers {
			if g.fingers[i].active && g.fingers[i].id == e.PointerID {
				g.fingers[i] = pinchFinger{}
			}
		}
	}
	return 0, f32.Point{}, f32.Point{}, false
}
