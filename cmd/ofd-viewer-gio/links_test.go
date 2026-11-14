package main

import (
	"bytes"
	"context"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/creator"
)

func TestWebURLOnlyAllowsAbsoluteHTTPAndHTTPS(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"https://example.test/a?q=1#part", "https://example.test/a?q=1#part"},
		{"HTTP://example.test", "http://example.test"},
		{"https://example.test/中文", "https://example.test/%E4%B8%AD%E6%96%87"},
		{"javascript:alert(1)", ""},
		{"file:///tmp/document", ""},
		{"mailto:user@example.test", ""},
		{"//example.test", ""},
		{"/relative", ""},
		{"https:///missing-host", ""},
		{"https://user:password@example.test", ""},
		{"https://example.test/\n", ""},
		{"https://example.test/%ZZ", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := webURL(tc.raw)
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("webURL(%q) = %q/%v", tc.raw, got, ok)
			}
		})
	}
}

func TestDocumentCachesResolvedWebLinksWithPageScene(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "gio-links", PageSize: creator.A4, Pages: []creator.Page{
		{Items: []creator.Item{
			creator.Path{X: 10, Y: 20, Width: 30, Height: 10, Data: "M 0 0 L 30 0 L 30 10 C", Fill: true,
				Actions: []creator.Action{{URI: &creator.URIAction{URI: "chapter?q=1", Base: "https://example.test/docs/"}}}},
			creator.Path{X: 50, Y: 20, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 C", Fill: true,
				Actions: []creator.Action{{URI: &creator.URIAction{URI: "file:///tmp/document"}}}},
			creator.Path{X: 70, Y: 20, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 C", Fill: true,
				Actions: []creator.Action{{Goto: &creator.GotoAction{Bookmark: "chapter"}}}},
		}},
		{},
	}, Bookmarks: []creator.Bookmark{{Name: "chapter", Goto: creator.GotoAction{Page: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := openDocumentInput("links.ofd", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	scene, ok := m.scene.(*linkedPageScene)
	if !ok || len(scene.links) != 2 || scene.links[0].uri != "https://example.test/docs/chapter?q=1" || scene.links[1].target == nil || scene.links[1].target.Page != 1 {
		t.Fatalf("页面未提取安全网址: %#v", m.scene)
	}
	if !scene.links[0].contains(f32.Pt(20, 25)) || scene.links[0].contains(f32.Pt(20, 35)) {
		t.Fatal("链接未保持从页顶向下的毫米坐标")
	}
	weighted, ok := scene.pageScene.(interface{ Weight() int64 })
	if !ok || scene.Weight() <= weighted.Weight() {
		t.Fatal("链接数据未计入缓存重量")
	}
	if err := m.selectPage(1); err != nil {
		t.Fatal(err)
	}
	batch, err := loadPageBatch(t.Context(), m, []int{0})
	if err != nil || len(batch) != 1 || batch[0].err != nil || batch[0].scene != scene {
		t.Fatalf("连续页未复用带链接的缓存: %v / %+v", err, batch)
	}
}

func TestPageLinkPointerRejectsDragCancelWrongButtonAndOutsideRelease(t *testing.T) {
	links := newLinkedPageScene(&countedScene{Canvas: canvas.New(100, 100)}, []drawing.PageLink{{URI: "https://example.test", X: 10, Y: 10, Width: 40, Height: 40}}).links
	for _, tc := range []struct {
		name   string
		button pointer.Buttons
		events []pointer.Event
		want   int
	}{
		{"点击", pointer.ButtonPrimary, nil, 1},
		{"轻微抖动", pointer.ButtonPrimary, []pointer.Event{{Kind: pointer.Drag, Position: f32.Pt(21, 20)}}, 1},
		{"拖动再返回", pointer.ButtonPrimary, []pointer.Event{{Kind: pointer.Drag, Position: f32.Pt(30, 20)}}, 0},
		{"取消", pointer.ButtonPrimary, []pointer.Event{{Kind: pointer.Cancel}}, 0},
		{"离开页面裁剪区", pointer.ButtonPrimary, []pointer.Event{{Kind: pointer.Leave}}, 0},
		{"右键", pointer.ButtonSecondary, nil, 0},
		{"在外部释放", pointer.ButtonPrimary, []pointer.Event{{Kind: pointer.Release, Position: f32.Pt(80, 80)}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p pageLinkPointer
			opened := 0
			open := func(pageLink) { opened++ }
			p.handle(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: tc.button, Position: f32.Pt(20, 20)}, links, 2, 1, open)
			for _, e := range tc.events {
				p.handle(e, links, 2, 1, open)
			}
			p.handle(pointer.Event{Kind: pointer.Release, Position: f32.Pt(20, 20)}, links, 2, 1, open)
			if opened != tc.want {
				t.Fatalf("打开次数 = %d，期望 %d", opened, tc.want)
			}
		})
	}
}

func TestWebLinkHonorsNonRectangularRegion(t *testing.T) {
	s := newLinkedPageScene(&countedScene{Canvas: canvas.New(100, 100)}, []drawing.PageLink{{
		URI: "https://example.test", X: 10, Y: 20, Width: 40, Height: 20, Region: "M0 0 L1 0 L0 1 z",
	}})
	if len(s.links) != 1 || !s.links[0].contains(f32.Pt(14, 22)) || s.links[0].contains(f32.Pt(46, 38)) {
		t.Fatal("非矩形启动区域被扩大为外接矩形")
	}
}

// 使用真实 Gio 事件路由验证各阅读模式，不能只验证热区状态函数。
func TestWebLinksOpenThroughGioRouterInAllReadingModes(t *testing.T) {
	for _, mode := range []string{"单页", "连续", "双页"} {
		t.Run(mode, func(t *testing.T) {
			scene := newLinkedPageScene(&countedScene{Canvas: canvas.New(100, 100)}, []drawing.PageLink{{URI: "https://example.test", X: 10, Y: 10, Width: 40, Height: 40}})
			v := viewer{scene: scene, total: 2, viewport: pageViewport{mode: fitManual, scale: 2}}
			var opened []string
			v.openURL = func(uri string) { opened = append(opened, uri) }
			v.continuous.double = mode == "双页"
			v.continuous.reset(0)
			for i := range 2 {
				v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{100, 100}})
				v.continuous.entries.AddWeighted(i, &continuousEntry{scene: scene}, scene.Weight())
			}
			var router input.Router
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(240, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			th := continuousTestTheme()
			frame := func() {
				ops.Reset()
				if mode == "单页" {
					v.layoutViewport(gtx)
				} else {
					v.layoutContinuous(gtx, th)
				}
				router.Frame(&ops)
			}
			frame()
			// 单页和连续页居中；双页宽于视口，选择右页并覆盖页间距。
			point := f32.Pt(60, 40)
			if mode == "双页" {
				point = f32.Pt(252, 40)
				v.continuous.panX = -40
				point.X -= 40
				frame()
			}
			router.Queue(pointer.Event{Kind: pointer.Move, Position: point})
			frame()
			if got := router.Cursor(); got != pointer.CursorPointer {
				t.Fatalf("悬停光标 = %v", got)
			}
			router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: point})
			frame()
			router.Queue(pointer.Event{Kind: pointer.Release, Position: point})
			frame()
			if len(opened) != 1 || opened[0] != "https://example.test" {
				t.Fatalf("点击未打开网址: %v", opened)
			}
			router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: point})
			frame()
			router.Queue(pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: point.Add(f32.Pt(-20, -20))})
			frame()
			router.Queue(pointer.Event{Kind: pointer.Release, Position: point})
			frame()
			if len(opened) != 1 {
				t.Fatal("拖动误打开了网址")
			}
		})
	}
}

func TestLaunchWebURLIgnoresUnsafeSchemes(t *testing.T) {
	results := make(chan error, 1)
	launchWebURL(context.Background(), "javascript:alert(1)", results, func() { t.Error("不应执行打开操作") })
	select {
	case err := <-results:
		t.Fatalf("不安全地址不应启动外部程序: %v", err)
	default:
	}
}

func TestWebLinksUpdateHoverAfterScrollWithoutMouseMovement(t *testing.T) {
	for _, mode := range []string{"单页", "连续"} {
		t.Run(mode, func(t *testing.T) {
			scene := newLinkedPageScene(&countedScene{Canvas: canvas.New(100, 100)}, []drawing.PageLink{{URI: "https://example.test", X: 10, Y: 10, Width: 40, Height: 40}})
			v := viewer{scene: scene, total: 1, viewport: pageViewport{mode: fitManual, scale: 2}}
			v.continuous.reset(0)
			v.continuous.metadata = []continuousPageMetadata{{size: continuousPageSize{100, 100}}}
			v.continuous.entries.AddWeighted(0, &continuousEntry{scene: scene}, scene.Weight())
			var router input.Router
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(240, 140)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
			th := continuousTestTheme()
			frame := func() {
				ops.Reset()
				if mode == "单页" {
					v.layoutViewport(gtx)
				} else {
					v.layoutContinuous(gtx, th)
				}
				router.Frame(&ops)
			}
			frame()
			point := f32.Pt(60, 60)
			router.Queue(pointer.Event{Kind: pointer.Move, Position: point})
			frame()
			if got := router.Cursor(); got != pointer.CursorPointer {
				t.Fatalf("链接上未显示手形: %v", got)
			}
			router.Queue(pointer.Event{Kind: pointer.Scroll, Position: point, Scroll: f32.Pt(0, 60)})
			frame()
			frame()
			if got := router.Cursor(); got != pointer.CursorGrab {
				t.Fatalf("滚动移走链接后仍显示旧光标: %v", got)
			}
			router.Queue(pointer.Event{Kind: pointer.Scroll, Position: point, Scroll: f32.Pt(0, -60)})
			frame()
			frame()
			if got := router.Cursor(); got != pointer.CursorPointer {
				t.Fatalf("链接滚回鼠标下后未恢复手形: %v", got)
			}
		})
	}
}
