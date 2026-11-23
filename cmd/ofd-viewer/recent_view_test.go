package main

import (
	"fmt"
	"image"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestRecentOverlayDoesNotResizeReadingViewport(t *testing.T) {
	for _, width := range []int{320, 900} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			h := newRecentTestHarness(t, image.Pt(width, 700))
			before := h.v.viewport.viewport
			h.open()
			if h.v.viewport.viewport != before {
				t.Fatalf("最近文档浮层挤压了阅读区: before=%v after=%v", before, h.v.viewport.viewport)
			}
			if _, ok := navigationSemanticBounds(&h.router, h.v.recentFiles[0]); !ok {
				t.Fatal("最近文档行没有显示")
			}
		})
	}
}

func TestRecentOverlayIconNameAndDirectoryShareOpenTarget(t *testing.T) {
	for _, part := range []string{"图标", "文件名", "目录"} {
		t.Run(part, func(t *testing.T) {
			h := newRecentTestHarness(t, image.Pt(900, 700))
			h.open()
			path := h.v.recentFiles[0]
			bounds, ok := navigationSemanticBounds(&h.router, path)
			if !ok || bounds.Empty() {
				t.Fatal("缺少最近文档整行热区")
			}
			point := bounds.Min.Add(image.Pt(80, 17))
			switch part {
			case "图标":
				point = bounds.Min.Add(image.Pt(20, bounds.Dy()/2))
			case "目录":
				point = bounds.Min.Add(image.Pt(80, bounds.Dy()-15))
			}
			h.click(point)
			if h.v.showRecent || len(h.requests) != 1 || h.requests[0].filename != path {
				t.Fatalf("整行点击未打开原始路径: part=%s requests=%+v open=%v", part, h.requests, h.v.showRecent)
			}
		})
	}
}

func TestRecentOverlayDismissesWithoutActivatingUnderlyingContent(t *testing.T) {
	for _, action := range []string{"外部点击", "关闭按钮", "Esc", "再次点击图标", "Android返回", "打开菜单"} {
		t.Run(action, func(t *testing.T) {
			h := newRecentTestHarness(t, image.Pt(900, 700))
			h.open()
			switch action {
			case "外部点击":
				h.click(image.Pt(850, 400))
			case "关闭按钮":
				bounds, ok := navigationSemanticBounds(&h.router, "关闭最近文档")
				if !ok {
					t.Fatal("缺少关闭按钮")
				}
				h.click(bounds.Min.Add(bounds.Size().Div(2)))
			case "Esc":
				h.router.Queue(key.Event{Name: key.NameEscape, State: key.Press})
				h.frame()
			case "再次点击图标":
				h.v.recentButton.Click()
				h.frame()
			case "Android返回":
				h.v.handleAndroidBack(h.gtx, func(request) { t.Fatal("不能关闭文档") }, func() { t.Fatal("不能退出阅读器") })
			case "打开菜单":
				h.v.menu.button.Click()
				h.frame()
			}
			if h.v.showRecent || h.exited || len(h.requests) != 0 || h.v.viewport.dragging || h.v.total != 2 {
				t.Fatalf("关闭浮层误触正文或退出: open=%v exited=%v requests=%+v dragging=%v", h.v.showRecent, h.exited, h.requests, h.v.viewport.dragging)
			}
		})
	}
}

func TestRecentOverlayClearOnlyClearsHistoryAndEmptyStateKeepsClose(t *testing.T) {
	h := newRecentTestHarness(t, image.Pt(900, 700))
	h.open()
	h.v.clearRecent.Click()
	h.frame()
	if len(h.v.recentFiles) != 0 || h.v.showRecent || h.persisted != 1 || h.v.scene == nil || h.v.total != 2 {
		t.Fatal("清空历史不能关闭文档或保留旧列表")
	}
	h.open()
	if _, ok := navigationSemanticBounds(&h.router, "关闭最近文档"); !ok {
		t.Fatal("空列表仍需关闭入口")
	}
	h.v.clearRecent.Click()
	h.frame()
	if h.persisted != 1 {
		t.Fatal("空列表不能重复清空或保存")
	}
}

func TestRecentOverlayScrollDoesNotPanDocumentAndListRemainsScrollable(t *testing.T) {
	h := newRecentTestHarness(t, image.Pt(900, 420))
	for i := 1; i < maxRecentFiles; i++ {
		h.v.recentFiles = append(h.v.recentFiles, filepath.Join(t.TempDir(), fmt.Sprintf("%d.ofd", i)))
	}
	h.open()
	offset, first := h.v.viewport.offset, h.v.recentList.Position.First
	h.router.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(850, 200), Scroll: f32.Pt(0, 80)})
	h.frame()
	if h.v.viewport.offset != offset || h.v.scrollOverflow != 0 {
		t.Fatal("浮层外部滚轮传到了正文")
	}
	h.router.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(200, 200), Scroll: f32.Pt(0, 200)})
	h.frame()
	h.frame()
	if h.v.recentList.Position.First <= first || h.v.viewport.offset != offset {
		t.Fatalf("浮层列表无法滚动或带动正文: first=%d position=%+v", first, h.v.recentList.Position)
	}
	h.v.showRecent = false
	h.open()
	if h.v.recentList.Position.First != 0 {
		t.Fatal("重新打开应返回最近一项")
	}
}

func TestRecentOverlayFrameFitsNarrowShortAndHighDPIWindows(t *testing.T) {
	for _, size := range []image.Point{image.Pt(900, 700), image.Pt(320, 250), image.Pt(200, 150), image.Pt(10, 10)} {
		for _, scale := range []float32{1, 2} {
			frame := recentFilesFrame(size, 70, 8, scale)
			if !frame.Empty() && !frame.In(image.Rectangle{Max: size}) {
				t.Fatalf("浮层超出窗口: size=%v scale=%v frame=%v", size, scale, frame)
			}
		}
	}
}

func TestRecentCurrentMarkerUsesAbsolutePathWithoutFilesystemAccess(t *testing.T) {
	v := viewer{total: 1}
	v.thumbnails.filename = "recent.ofd"
	absolute, err := filepath.Abs(v.thumbnails.filename)
	if err != nil {
		t.Fatal(err)
	}
	if !v.isCurrentRecentFile(absolute) || v.isCurrentRecentFile(absolute+"-other") {
		t.Fatal("从相对路径打开的文档也应识别当前记录")
	}
	v.total = 0
	if v.isCurrentRecentFile(absolute) {
		t.Fatal("关闭文档后不能继续标记当前记录")
	}
}

func TestRecentOverlayGPUShowsNeutralCardCurrentAndHoverStates(t *testing.T) {
	window, err := headless.NewWindow(900, 700)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	h := newRecentTestHarness(t, image.Pt(900, 700))
	h.open()
	bounds, ok := navigationSemanticBounds(&h.router, h.v.recentFiles[0])
	if !ok {
		t.Fatal("缺少最近文档项")
	}
	if err := window.Frame(&h.ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 900, 700))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	point := bounds.Min.Add(image.Pt(3, bounds.Dy()/2))
	current := pixels.RGBAAt(point.X, point.Y)
	if current.R < 210 || current.B < current.R+10 {
		t.Fatalf("当前文档应使用浅蓝背景，而非大面积实色按钮: %v", current)
	}
	h.router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(point.X), float32(point.Y))})
	h.frame()
	h.frame()
	if err := window.Frame(&h.ops); err != nil {
		t.Fatal(err)
	}
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	if hovered := pixels.RGBAAt(point.X, point.Y); hovered.R >= current.R {
		t.Fatalf("悬停背景没有清晰变化: normal=%v hovered=%v", current, hovered)
	}
}

type recentTestHarness struct {
	v         viewer
	router    input.Router
	ops       op.Ops
	gtx       layout.Context
	frame     func()
	requests  []request
	exited    bool
	persisted int
}

func newRecentTestHarness(t *testing.T, size image.Point) *recentTestHarness {
	t.Helper()
	h := &recentTestHarness{v: viewer{total: 2, scene: &countedScene{Canvas: canvas.New(210, 297)},
		viewport: pageViewport{mode: fitManual, scale: 4}}}
	path := filepath.Join(t.TempDir(), strings.Repeat("长目录", 20), strings.Repeat("长文件名", 20)+".ofd")
	h.v.recentFiles, h.v.thumbnails.filename = []string{path}, path
	h.gtx = layout.Context{Ops: &h.ops, Source: h.router.Source(), Constraints: layout.Exact(size), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	h.frame = func() {
		h.ops.Reset()
		h.v.handleMenu(h.gtx, func() { h.exited = true })
		h.v.handleKeys(h.gtx, func(req request) { h.requests = append(h.requests, req) }, func() { h.exited = true })
		h.v.handleRecent(h.gtx, func() { h.persisted++ }, func(req request) { h.requests = append(h.requests, req) })
		h.v.layout(h.gtx, th)
		h.router.Frame(&h.ops)
	}
	h.frame()
	return h
}

func (h *recentTestHarness) open() {
	h.v.recentButton.Click()
	h.frame()
}

func (h *recentTestHarness) click(point image.Point) {
	position := f32.Pt(float32(point.X), float32(point.Y))
	h.router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: position})
	h.router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: position})
	h.frame()
}
