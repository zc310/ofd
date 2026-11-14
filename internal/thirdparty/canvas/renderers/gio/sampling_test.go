package gio

import (
	"math"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPaintFallbackResolutionFollowsExplicitTargetAndKeepsDefault(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 20, 10)
	bounds := canvas.Rect{X1: 10, Y1: 5}
	if got := r.paintResolution(bounds); got != 150.0/25.4 {
		t.Fatalf("未指定采样密度时应保留默认值: %v", got)
	}
	for _, density := range []float64{2, 12, 24} {
		r.SetRasterResolution(canvas.Resolution(density))
		if got := r.paintResolution(bounds); got != density {
			t.Fatalf("未跟随目标像素密度: got=%v want=%v", got, density)
		}
	}
	for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		r.SetRasterResolution(canvas.Resolution(invalid))
		if got := r.paintResolution(bounds); got != 150.0/25.4 {
			t.Fatalf("非法采样密度应恢复默认值: input=%v got=%v", invalid, got)
		}
	}
}

func TestPaintFallbackHighResolutionRetainsPixelBudget(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 2000, 2000)
	r.SetRasterResolution(canvas.DPMM(1000))
	for _, bounds := range []canvas.Rect{{X1: 20, Y1: 10}, {X1: 2000, Y1: 2000}, {X1: 0.1, Y1: 1000}} {
		density := r.paintResolution(bounds)
		if density <= 0 || density*bounds.W() > 2048.000001 || density*bounds.H() > 2048.000001 {
			t.Fatalf("高 DPI 不能突破单图 2048×2048 预算: bounds=%v density=%v", bounds, density)
		}
	}
}
