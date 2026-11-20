package main

import (
	"image"
	"math"
	"testing"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestDefaultWindowReadingAreaFitsA4(t *testing.T) {
	th := continuousTestTheme()
	width, height := defaultWindowSize(th)
	if width != 700 || height <= width {
		t.Fatalf("启动窗口尺寸异常: %v × %v", width, height)
	}
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(int(width), int(height))), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, total: 1}
	v.layout(gtx, th)
	viewport := v.viewport.viewport
	wantHeight := float64(viewport.X) * 297 / 210
	if math.Abs(float64(viewport.Y)-wantHeight) > 1.1 {
		t.Fatalf("阅读区应为 A4 比例: viewport=%v wantHeight=%.2f", viewport, wantHeight)
	}
}

func TestDefaultSizeTracksActualToolbarHeight(t *testing.T) {
	th := continuousTestTheme()
	_, height := defaultWindowSize(th)
	th.TextSize *= 1.5
	_, largeHeight := defaultWindowSize(th)
	if largeHeight <= height {
		t.Fatalf("较大字号应计入工具栏和状态栏: normal=%v large=%v", height, largeHeight)
	}
}
