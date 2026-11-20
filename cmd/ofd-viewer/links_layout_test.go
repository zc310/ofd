package main

import (
	"fmt"
	"image"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 覆盖完整窗口布局及真实鼠标悬停、按住、松开，防止局部阅读区测试遗漏叠层和帧调度。
func TestPageLinksReceiveMillimeterCoordinatesInFullWindowLayout(t *testing.T) {
	for _, mode := range []string{"单页", "连续", "双页"} {
		for _, kind := range []string{"内部跳转", "外部网址"} {
			for _, scale := range []float32{0.75, 2.3, 4} {
				t.Run(fmt.Sprintf("%s/%s/%.2f", mode, kind, scale), func(t *testing.T) {
					target := drawing.LinkTarget{Page: 1, Type: drawing.DestFit}
					link := drawing.PageLink{Target: &target, X: 17, Y: 18, Width: 8, Height: 3}
					if kind == "外部网址" {
						link.Target, link.URI = nil, "https://example.test"
					}
					scene := newLinkedPageScene(&countedScene{Canvas: canvas.New(210.5, 297.25)}, []drawing.PageLink{link})
					v := viewer{scene: scene, viewScene: scene, total: 2, viewport: pageViewport{mode: fitManual, scale: scale}}
					var opened []string
					v.openURL = func(uri string) { opened = append(opened, uri) }
					v.continuous.enabled = mode != "单页"
					v.continuous.double = mode == "双页"
					v.continuous.reset(0)
					for i := range 2 {
						v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{210.5, 297.25}})
						v.continuous.entries.AddWeighted(i, &continuousEntry{scene: scene}, scene.Weight())
					}
					var router input.Router
					var ops op.Ops
					gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(850, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
					th := continuousTestTheme()
					var requested []request
					submit := func(req request) { requested = append(requested, req) }
					frame := func() {
						ops.Reset()
						v.processLinkJump(submit)
						v.handleMenu(gtx, func() {})
						v.handleKeys(gtx, submit, func() {})
						v.handlePageInput(gtx, submit)
						v.layout(gtx, th)
						router.Frame(&ops)
					}
					frame()
					margin := gtx.Dp(readingMargin)
					point := f32.Pt(float32(margin)+float32(math.Round(float64(v.viewport.offset.X)))+20*scale,
						float32(600-margin-v.viewport.viewport.Y)+float32(math.Round(float64(v.viewport.offset.Y)))+20*scale)
					if v.continuous.enabled {
						width := float32(math.Round(210.5 * float64(scale)))
						if v.continuous.double {
							width = width*2 + float32(gtx.Dp(unit.Dp(12)))
						}
						point.X = float32(margin) + float32(math.Round(float64(v.continuous.pageX(width, float32(v.viewport.viewport.X))))) + 20*scale
						point.Y = float32(600-margin-v.viewport.viewport.Y) + 20*scale
					}
					router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: point})
					frame()
					if router.Cursor() != pointer.CursorPointer {
						t.Fatalf("测试点击位置不在链接上: point=%v viewport=%+v cursor=%v", point, v.viewport, router.Cursor())
					}
					router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: point})
					frame()
					pressedAt := v.pageLinks.start
					if v.continuous.enabled {
						entry, ok := v.continuous.entries.Get(0)
						if !ok {
							t.Fatal("点击页不在缓存中")
						}
						pressedAt = entry.links.start
					}
					if math.Hypot(float64(pressedAt.X-20), float64(pressedAt.Y-20)) > 1e-3 {
						t.Fatalf("点击事件没有还原为页面毫米坐标: %v", pressedAt)
					}
					frame()
					frame()
					router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: point})
					frame()
					frame()
					if kind == "外部网址" {
						if len(opened) != 1 || opened[0] != link.URI || len(requested) != 0 {
							t.Fatalf("完整布局点击未打开网址或误提交跳页: opened=%v requests=%v", opened, requested)
						}
					} else if len(requested) != 1 || requested[0].page != 1 || len(opened) != 0 {
						t.Fatalf("完整布局点击未提交跳转: requests=%v jump=%+v", requested, v.linkJump)
					}
				})
			}
		}
	}
}
