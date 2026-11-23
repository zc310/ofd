package main

import (
	"fmt"
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
)

func TestMouseHistoryRestoresBookmarkSourceAndDestinationInAllReadingModes(t *testing.T) {
	for _, mode := range []string{"单页", "连续", "双页"} {
		for _, onSidebar := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/侧栏%v", mode, onSidebar), func(t *testing.T) {
				h := newHistoryTestHarness(mode)
				h.v.navigation.open = onSidebar
				h.frame()
				before := h.v.currentLocation()
				h.jump(3, 110)
				after := h.v.currentLocation()
				if len(h.v.history.back) != 1 || after.page != 3 || after == before {
					t.Fatalf("书签跳转未记录来源: before=%+v after=%+v history=%+v", before, after, h.v.history)
				}
				h.sideButton(pointer.ButtonQuaternary, onSidebar)
				if got := h.v.currentLocation(); got != before {
					t.Fatalf("鼠标后退没有恢复来源位置: got=%+v want=%+v", got, before)
				}
				h.sideButton(pointer.ButtonQuinary, onSidebar)
				if got := h.v.currentLocation(); got != after {
					t.Fatalf("鼠标前进没有恢复目标位置: got=%+v want=%+v", got, after)
				}
				if h.v.viewport.dragging || h.v.continuous.dragging || h.v.linkJump != nil || h.v.linkDestination != nil {
					t.Fatal("侧键导航不能触发拖动或残留跳转")
				}
				if h.v.pageInput.Text() != "4" || h.v.thumbnails.list.Position.First != 3 {
					t.Fatal("前进后全局页码及缩略图没有同步")
				}
			})
		}
	}
}

func TestHistorySupportsSamePageBookmarksAndBranchesAfterBack(t *testing.T) {
	h := newHistoryTestHarness("单页")
	source := h.v.currentLocation()
	h.jump(0, 100)
	first := h.v.currentLocation()
	if first == source || len(h.v.history.back) != 1 {
		t.Fatal("同页书签定位也应记录来源")
	}
	h.jump(2, 80)
	h.sideButton(pointer.ButtonQuaternary, false)
	if h.v.currentLocation() != first || len(h.v.history.forward) != 1 {
		t.Fatal("多次跳转后退失败")
	}
	h.jump(1, 60)
	if len(h.v.history.forward) != 0 {
		t.Fatal("后退后访问新书签应清空前进分支")
	}
	before := h.v.currentLocation()
	h.sideButton(pointer.ButtonQuinary, false)
	if h.v.currentLocation() != before {
		t.Fatal("没有前进记录时不能改变阅读位置")
	}
}

func TestHistoryRepeatedRoundTripsDoNotGrowHistoryOrActivateLinks(t *testing.T) {
	h := newHistoryTestHarness("单页")
	initial := h.v.currentLocation()
	h.sideButton(pointer.ButtonQuaternary, false)
	if h.v.currentLocation() != initial {
		t.Fatal("空历史后退不能改变页面")
	}
	h.jump(3, 100)
	destination := h.v.currentLocation()
	for range 5 {
		h.sideButton(pointer.ButtonQuaternary, false)
		if h.v.currentLocation() != initial || len(h.v.history.back) != 0 || len(h.v.history.forward) != 1 {
			t.Fatal("后退不能新增普通跳转记录")
		}
		h.sideButton(pointer.ButtonQuinary, false)
		if h.v.currentLocation() != destination || len(h.v.history.back) != 1 || len(h.v.history.forward) != 0 {
			t.Fatal("反复前进后退不能扩张历史或误触链接")
		}
	}
}

func TestHistoryRestorationClampsAfterWindowResize(t *testing.T) {
	h := newHistoryTestHarness("单页")
	h.jump(3, 100)
	h.gtx.Constraints = layout.Exact(image.Pt(1200, 900))
	h.frame()
	h.sideButton(pointer.ButtonQuaternary, false)
	viewport := h.v.viewport
	clamped := viewport
	clamped.clamp()
	if h.v.page != 0 || viewport.scale != 5 || viewport.viewport.X <= 900 || viewport.offset != clamped.offset {
		t.Fatalf("窗口变化后应恢复缩放并适配可滚动范围: %+v", viewport)
	}
}

func TestHistoryOnlyCommitsSuccessfulInternalNavigation(t *testing.T) {
	h := newHistoryTestHarness("单页")
	h.jump(2, 80)
	h.sideButton(pointer.ButtonQuaternary, false)
	back, forward := len(h.v.history.back), len(h.v.history.forward)
	h.v.openURL = func(string) {}
	h.v.activatePageLink(pageLink{uri: "https://example.test"})
	h.frame()
	h.v.activatePageLink(pageLink{target: &drawing.LinkTarget{Page: 99, Type: drawing.DestFit}})
	h.frame()
	h.v.activatePageLink(pageLink{target: &drawing.LinkTarget{Page: 1, Type: drawing.DestXYZ, Top: 100}})
	h.v.processLinkJump(func(request) {
		h.v.acceptResponse(result{scene: h.scenes[h.v.page], page: h.v.page, total: 4, err: errPageOperation}, func() {}, func(string) {})
	})
	if len(h.v.history.back) != back || len(h.v.history.forward) != forward || h.v.linkDestination != nil {
		t.Fatal("外部链接、失效目标或加载失败不能修改导航历史")
	}
	// 历史恢复失败后仍可重试，不能提前弹出前进记录。
	h.v.navigateHistory(1)
	h.v.processLinkJump(func(request) {
		h.v.acceptResponse(result{scene: h.scenes[h.v.page], page: h.v.page, total: 4, err: errPageOperation}, func() {}, func(string) {})
	})
	if len(h.v.history.forward) != forward {
		t.Fatal("前进失败不能丢失原历史记录")
	}
}

func TestHistoryRestoresReadingModeAcrossModeChanges(t *testing.T) {
	h := newHistoryTestHarness("单页")
	original := h.v.currentLocation()
	h.jump(3, 110)
	h.v.continuous.enabled, h.v.continuous.double = true, true
	h.frame()
	continuous := h.v.currentLocation()
	h.sideButton(pointer.ButtonQuaternary, false)
	if h.v.currentLocation() != original || h.v.scene != h.scenes[0] {
		t.Fatalf("连续模式后退到单页时必须加载对应页面: got=%+v want=%+v", h.v.currentLocation(), original)
	}
	h.sideButton(pointer.ButtonQuinary, false)
	if h.v.currentLocation() != continuous {
		t.Fatalf("前进时没有恢复双页视图: got=%+v want=%+v", h.v.currentLocation(), continuous)
	}
}

func TestHistoryMouseButtonsAreIgnoredDuringLoadingAndModalDialogs(t *testing.T) {
	for _, state := range []string{"忙碌", "菜单", "关于", "最近文档", "解锁", "待跳转", "触屏", "左右键"} {
		t.Run(state, func(t *testing.T) {
			h := newHistoryTestHarness("单页")
			h.jump(3, 100)
			switch state {
			case "忙碌":
				h.v.busy = true
			case "菜单":
				h.v.menu.open = true
			case "关于":
				h.v.menu.about = true
			case "最近文档":
				h.v.showRecent = true
			case "解锁":
				h.v.menu.unlockOpen = true
			case "待跳转":
				h.v.linkDestination = &linkNavigation{}
			}
			before := h.v.currentLocation()
			source, button := pointer.Mouse, pointer.ButtonQuaternary
			if state == "触屏" {
				source = pointer.Touch
			}
			if state == "左右键" {
				button = pointer.ButtonSecondary
			}
			h.router.Queue(pointer.Event{Kind: pointer.Press, Source: source, Buttons: button, Position: f32.Pt(700, 350)})
			h.frame()
			h.router.Queue(pointer.Event{Kind: pointer.Release, Source: source, Position: f32.Pt(700, 350)})
			h.frame()
			if h.v.currentLocation() != before || len(h.v.history.back) != 1 {
				t.Fatal("忙碌、弹层、触屏或非侧键不能消费导航历史")
			}
		})
	}
}

func TestHistoryIsBoundedAndResetsOnlyWhenDocumentChanges(t *testing.T) {
	var stack []navigationLocation
	for page := range navigationHistoryLimit + 20 {
		stack = appendNavigationLocation(stack, navigationLocation{page: page})
	}
	if len(stack) != navigationHistoryLimit || stack[0].page != 20 {
		t.Fatal("导航历史没有按上限淘汰最旧记录")
	}
	h := newHistoryTestHarness("单页")
	h.jump(3, 100)
	for _, failed := range []bool{false, true} {
		h.v.pendingOpen = true
		response := result{scene: h.v.scene, filename: "same.ofd", page: 3, total: 4}
		if failed {
			response.err = errPageOperation
		}
		h.v.acceptResponse(response, func() {}, func(string) {})
		if len(h.v.history.back) != 1 {
			t.Fatal("打开失败或取消不能清空历史")
		}
	}
	h.v.pendingOpen = true
	h.v.acceptResponse(result{scene: &countedScene{Canvas: canvas.New(210, 297)}, filename: "new.ofd", total: 1}, func() {}, func(string) {})
	if len(h.v.history.back) != 0 || len(h.v.history.forward) != 0 {
		t.Fatal("换文档必须清空导航历史")
	}
	h.v.history.back = stack
	h.v.closeDocument(func(request) {})
	if len(h.v.history.back) != 0 {
		t.Fatal("关闭文档必须释放历史")
	}
}

type historyTestHarness struct {
	v      viewer
	scenes []pageScene
	router input.Router
	ops    op.Ops
	gtx    layout.Context
	frame  func()
}

func newHistoryTestHarness(mode string) *historyTestHarness {
	h := new(historyTestHarness)
	for range 4 {
		h.scenes = append(h.scenes, &countedScene{Canvas: canvas.New(210, 297)})
	}
	h.v = viewer{scene: h.scenes[0], viewScene: h.scenes[0], total: 4,
		viewport: pageViewport{mode: fitManual, scale: 5}}
	h.v.continuous.enabled, h.v.continuous.double = mode != "单页", mode == "双页"
	h.v.continuous.reset(0)
	for page, scene := range h.scenes {
		h.v.continuous.metadata = append(h.v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{210, 297}})
		h.v.continuous.entries.AddWeighted(page, &continuousEntry{scene: scene}, 1)
	}
	h.gtx = layout.Context{Ops: &h.ops, Source: h.router.Source(), Constraints: layout.Exact(image.Pt(900, 700)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	h.frame = func() {
		h.ops.Reset()
		h.v.handleHistoryPointer(h.gtx)
		h.v.processLinkJump(func(req request) {
			if h.v.continuous.enabled && !req.forcePage {
				h.v.continuous.jump(req.page)
				h.v.page = req.page
			} else {
				h.v.acceptResponse(result{scene: h.scenes[req.page], page: req.page, total: 4}, func() {}, func(string) {})
			}
		})
		h.v.layout(h.gtx, th)
		h.router.Frame(&h.ops)
	}
	h.frame()
	if h.v.continuous.enabled {
		h.v.continuous.list.Position.Offset = 90
		h.v.continuous.panX = -50
	} else {
		h.v.viewport.offset = f32.Pt(-50, -90)
	}
	h.frame()
	return h
}

func (h *historyTestHarness) jump(page int, top float64) {
	h.v.activatePageLink(pageLink{target: &drawing.LinkTarget{Page: page, Type: drawing.DestXYZ, Top: top}})
	h.frame()
}

func (h *historyTestHarness) sideButton(button pointer.Buttons, sidebar bool) {
	point := f32.Pt(700, 350)
	if sidebar {
		point.X = 120
	}
	h.router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: button, Position: point})
	h.frame()
	h.router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: point})
	h.frame()
}
