package canvas

import (
	"testing"

	"github.com/tdewolff/canvas/renderers/pdf"
	"github.com/zc310/ofd/internal/render/drawing"
)

// TestDestSpecMapsEveryType 守住 OFD 的五种跳转类型逐类对应到 canvas 的目的地类型，
// 不做合并。
func TestDestSpecMapsEveryType(t *testing.T) {
	cases := []struct {
		name   string
		target drawing.LinkTarget
		want   pdf.DestKind
	}{
		{"整页适配", drawing.LinkTarget{Type: drawing.DestFit}, pdf.DestFit},
		{"高度适配", drawing.LinkTarget{Type: drawing.DestFitH, Top: 100}, pdf.DestFitH},
		{"宽度适配", drawing.LinkTarget{Type: drawing.DestFitV, Left: 50}, pdf.DestFitV},
		{"定位到点", drawing.LinkTarget{Type: drawing.DestXYZ, Left: 50, Top: 100}, pdf.DestXYZ},
		{"缩放到矩形", drawing.LinkTarget{Type: drawing.DestFitR}, pdf.DestFitR},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := destSpec(c.target).Kind; got != c.want {
				t.Fatalf("目的地类型 = %v，期望 %v", got, c.want)
			}
		})
	}
}

// TestDestSpecKeepsOriginZeroCases 守住坐标为 0 时类型不被改变。
//
// 早先实现把 OFD 目标压成一个矩形交给 canvas，由矩形形状反推目的地类型，于是
// Left 为 0 的 XYZ 被判成 FitH、Left 为 0 的 FitV 被判成整页 Fit。改用显式类型后
// 这两种情况都能如实表达，因此这里作为正向断言固定下来。
func TestDestSpecKeepsOriginZeroCases(t *testing.T) {
	xyz := destSpec(drawing.LinkTarget{Type: drawing.DestXYZ, Left: 0, Top: 100})
	if xyz.Kind != pdf.DestXYZ {
		t.Fatalf("Left 为 0 的 XYZ 变成了 %v", xyz.Kind)
	}
	if xyz.X != 0 || xyz.Y != 100 {
		t.Fatalf("坐标被改动: %+v", xyz)
	}
	fitv := destSpec(drawing.LinkTarget{Type: drawing.DestFitV, Left: 0})
	if fitv.Kind != pdf.DestFitV {
		t.Fatalf("Left 为 0 的 FitV 变成了 %v", fitv.Kind)
	}
	if fitv.X != 0 {
		t.Fatalf("坐标被改动: %+v", fitv)
	}
}

// TestDestSpecKeepsZoom 守住 Dest@Zoom 不被丢弃。
func TestDestSpecKeepsZoom(t *testing.T) {
	for _, zoom := range []float64{0, 0.5, 1, 2.5} {
		got := destSpec(drawing.LinkTarget{Type: drawing.DestXYZ, Left: 10, Top: 20, Zoom: zoom})
		if got.Zoom != zoom {
			t.Errorf("Zoom = %v，期望 %v", got.Zoom, zoom)
		}
	}
}

// TestDestSpecCarriesCoordinatesUnflipped 守住坐标原样传递。
//
// canvas 的 Dest 以左上角为原点、由 AddDestToPage 按目标页高翻转，转换层若再翻一次
// 就会上下颠倒。
func TestDestSpecCarriesCoordinatesUnflipped(t *testing.T) {
	target := drawing.LinkTarget{
		Type: drawing.DestXYZ,
		Left: 10, Top: 20,
		PageHeight: 297,
	}
	got := destSpec(target)
	if got.X != 10 || got.Y != 20 {
		t.Fatalf("坐标 = %v,%v，期望原样传递 10,20", got.X, got.Y)
	}
}

// TestDestSpecMapsFitRRectangle 守住 FitR 的四个边都按 OFD 的命名传递：Left→X0、
// Top→Y1、Right→X1、Bottom→Y0。
func TestDestSpecMapsFitRRectangle(t *testing.T) {
	got := destSpec(drawing.LinkTarget{
		Type: drawing.DestFitR,
		Left: 10, Top: 20, Right: 110, Bottom: 60,
	})
	if got.X0 != 10 || got.Y1 != 20 || got.X1 != 110 || got.Y0 != 60 {
		t.Fatalf("FitR 矩形 = %+v", got)
	}
}
