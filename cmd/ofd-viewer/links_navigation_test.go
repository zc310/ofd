package main

import (
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

func TestLinkTargetViewportFollowsStandardDestinationTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target drawing.LinkTarget
		scale  float32
		offset f32.Point
	}{
		{"Fit", drawing.LinkTarget{Type: drawing.DestFit}, 1, f32.Pt(200, 0)},
		{"FitH按窗口宽度", drawing.LinkTarget{Type: drawing.DestFitH, Top: 100}, 2, f32.Pt(0, -200)},
		{"FitV按窗口高度且页窄时居中", drawing.LinkTarget{Type: drawing.DestFitV, Left: 50}, 1, f32.Pt(200, 0)},
		{"XYZ保持点击前缩放", drawing.LinkTarget{Type: drawing.DestXYZ, Left: 50, Top: 100}, 3, f32.Pt(-150, -300)},
		{"XYZ明确缩放并适配系统DPI", drawing.LinkTarget{Type: drawing.DestXYZ, Left: 50, Top: 100, Zoom: 2}, float32(96 / 25.4 * 4), f32.Pt(float32(-50*96/25.4*4), float32(-100*96/25.4*4))},
		{"FitR", drawing.LinkTarget{Type: drawing.DestFitR, Left: 100, Top: 200, Right: 300, Bottom: 350}, 4, f32.Pt(-400, -800)},
		{"FitR居中非等比目标", drawing.LinkTarget{Type: drawing.DestFitR, Left: 100, Top: 200, Right: 200, Bottom: 300}, 6, f32.Pt(-500, -1200)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := linkTargetViewport(tc.target, image.Pt(800, 600), f32.Pt(400, 600), 3, 2)
			if !ok || math.Abs(float64(got.scale-tc.scale)) > 1e-4 || math.Hypot(float64(got.offset.X-tc.offset.X), float64(got.offset.Y-tc.offset.Y)) > 1e-3 {
				t.Fatalf("目标视图 = %+v/%v，期望 scale=%v offset=%v", got, ok, tc.scale, tc.offset)
			}
		})
	}
	wide, ok := linkTargetViewport(drawing.LinkTarget{Type: drawing.DestFitV, Left: 100}, image.Pt(800, 600), f32.Pt(1000, 400), 3, 1)
	if !ok || wide.scale != 1.5 || wide.offset != f32.Pt(-150, 0) {
		t.Fatalf("FitV没有定位宽页面的Left: %+v", wide)
	}
	for _, target := range []drawing.LinkTarget{
		{Page: -1}, {Type: drawing.DestType(99)}, {Type: drawing.DestXYZ, Zoom: math.NaN()},
		{Type: drawing.DestXYZ, Top: math.Inf(1)}, {Type: drawing.DestXYZ, Zoom: -1},
		{Type: drawing.DestFitR, Left: 10, Right: 0, Bottom: 10},
	} {
		if _, ok := linkTargetViewport(target, image.Pt(800, 600), f32.Pt(400, 600), 3, 1); ok {
			t.Fatalf("接受了无效目标: %+v", target)
		}
	}
}

func TestInternalLinksDeferNavigationAndPreservePositionInAllReadingModes(t *testing.T) {
	for _, mode := range []string{"单页", "连续", "双页"} {
		t.Run(mode, func(t *testing.T) {
			target := drawing.LinkTarget{Page: 3, Type: drawing.DestXYZ, Left: 10, Top: 40}
			source := newLinkedPageScene(&countedScene{Canvas: canvas.New(100, 200)}, []drawing.PageLink{{Target: &target, X: 10, Y: 10, Width: 40, Height: 40}})
			v := viewer{scene: source, viewScene: source, total: 4, viewport: pageViewport{mode: fitManual, scale: 2}}
			v.openURL = func(string) { t.Fatal("内部跳转不得调用浏览器") }
			v.continuous.enabled = mode != "单页"
			v.continuous.double = mode == "双页"
			v.continuous.reset(0)
			scenes := []pageScene{source}
			for page := range 4 {
				scene := pageScene(source)
				if page > 0 {
					scene = &countedScene{Canvas: canvas.New(100, 200)}
					scenes = append(scenes, scene)
				}
				v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 200}})
				v.continuous.entries.AddWeighted(page, &continuousEntry{scene: scene}, 128)
			}
			var requested []int
			submit := func(req request) {
				requested = append(requested, req.page)
				if v.continuous.enabled {
					v.continuous.jump(req.page)
					v.page = req.page
				} else {
					v.acceptResponse(result{scene: scenes[req.page], page: req.page, total: 4}, func() {}, func(string) {})
				}
			}
			var router input.Router
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(240, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			th := continuousTestTheme()
			frame := func() {
				ops.Reset()
				v.processLinkJump(submit)
				if v.continuous.enabled {
					v.layoutContinuous(gtx, th)
				} else {
					v.layoutViewport(gtx)
				}
				router.Frame(&ops)
			}
			frame()
			point := f32.Pt(60, 40)
			if v.continuous.double {
				point.X = 40
			}
			router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: point})
			frame()
			router.Queue(pointer.Event{Kind: pointer.Release, Position: point})
			frame()
			if len(requested) != 0 || v.page != 0 || v.linkJump == nil {
				t.Fatal("页面重放期间提交了跳页，或内部链接未记录跳转意图")
			}
			frame()
			if len(requested) != 1 || requested[0] != 3 || v.page != 3 || v.pageInput.Text() != "4" || v.viewport.scale != 2 || v.linkDestination != nil {
				t.Fatalf("跳转页码、缩放或目标消费错误: requests=%v page=%d scale=%v pending=%+v", requested, v.page, v.viewport.scale, v.linkDestination)
			}
			if v.continuous.enabled {
				row := 3
				if v.continuous.double {
					row = 1
					if v.continuous.panX >= 0 {
						t.Fatal("双页跳转右页时未调整水平位置")
					}
				}
				if v.continuous.list.Position.First != row || v.continuous.list.Position.Offset != 80 {
					t.Fatalf("连续模式未落在目标页的位置: %+v", v.continuous.list.Position)
				}
			} else if v.viewport.offset.Y != -80 {
				t.Fatalf("单页未落在目标Top: %v", v.viewport.offset)
			}
		})
	}
}

func TestInternalSamePageJumpDoesNotReloadAndFailedJumpClearsDestination(t *testing.T) {
	v := viewer{total: 2, viewport: pageViewport{scale: 3}}
	v.activatePageLink(pageLink{target: &drawing.LinkTarget{Type: drawing.DestXYZ, Top: 40}})
	v.processLinkJump(func(request) { t.Fatal("同页跳转不应重新加载") })
	if v.linkDestination == nil || !v.linkDestination.ready || v.linkDestination.scale != 3 {
		t.Fatal("同页目标没有就绪或丢失原缩放")
	}
	v.activatePageLink(pageLink{target: &drawing.LinkTarget{Page: 1}})
	v.processLinkJump(func(request) {})
	v.acceptResponse(result{err: errPageOperation, total: 2}, func() {}, func(string) {})
	if v.linkDestination != nil {
		t.Fatal("切页失败后保留了过期目标")
	}
	v.activatePageLink(pageLink{target: &drawing.LinkTarget{Page: 99}})
	v.processLinkJump(func(request) { t.Fatal("无效目标不得发起切页") })
	if v.status == "" {
		t.Fatal("无效目标未给出提示")
	}
}

func TestInternalLinkJumpWaitsForBackgroundWorker(t *testing.T) {
	v := viewer{total: 2, viewport: pageViewport{scale: 3}}
	v.activatePageLink(pageLink{target: &drawing.LinkTarget{Page: 1}})
	v.busy = true
	v.processLinkJump(func(request) { t.Fatal("后台忙碌时不应提交新请求") })
	if v.linkJump == nil {
		t.Fatal("后台批次加载吞掉了已点击的跳转意图")
	}
	v.busy = false
	requested := -1
	v.processLinkJump(func(req request) { requested = req.page })
	if requested != 1 || v.linkJump != nil || v.linkDestination == nil {
		t.Fatal("后台空闲后未提交原跳转")
	}
}
