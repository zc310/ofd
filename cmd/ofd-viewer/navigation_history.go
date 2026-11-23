package main

import (
	"image"
	"strconv"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"github.com/zc310/ofd/internal/render/drawing"
)

const navigationHistoryLimit = 128

// 历史只保存视图标量，不持有页面、位图或文档会话。
type navigationLocation struct {
	page               int
	mode               fitMode
	scale              float32
	offset             f32.Point
	viewport           image.Point
	pageSize           f32.Point
	continuous, double bool
	first, scroll      int
	panX               float32
}

type navigationHistory struct {
	back, forward []navigationLocation
	buttons       pointer.Buttons
}

func (v *viewer) currentLocation() navigationLocation {
	return navigationLocation{
		page: v.page, mode: v.viewport.mode, scale: v.viewport.scale, offset: v.viewport.offset,
		viewport: v.viewport.viewport, pageSize: v.viewport.page,
		continuous: v.continuous.enabled, double: v.continuous.double,
		first: v.continuous.list.Position.First, scroll: v.continuous.list.Position.Offset, panX: v.continuous.panX,
	}
}

func appendNavigationLocation(stack []navigationLocation, location navigationLocation) []navigationLocation {
	if len(stack) > 0 && stack[len(stack)-1] == location {
		return stack
	}
	if len(stack) == navigationHistoryLimit {
		copy(stack, stack[1:])
		stack = stack[:len(stack)-1]
	}
	return append(stack, location)
}

// 仅在目标实际应用成功后提交历史；失败、取消及外部链接不能改变前进/后退栈。
func (v *viewer) commitNavigation(navigation *linkNavigation) {
	if navigation.source == nil {
		return
	}
	h := &v.history
	switch navigation.historyDirection {
	case -1:
		h.back = h.back[:len(h.back)-1]
		h.forward = appendNavigationLocation(h.forward, *navigation.source)
	case 1:
		h.forward = h.forward[:len(h.forward)-1]
		h.back = appendNavigationLocation(h.back, *navigation.source)
	default:
		if *navigation.source == v.currentLocation() {
			return
		}
		h.back = appendNavigationLocation(h.back, *navigation.source)
		h.forward = nil
	}
}

func (v *viewer) navigateHistory(direction int) {
	if v.busy || v.total == 0 || v.linkJump != nil || v.linkDestination != nil ||
		v.menu.open || v.menu.about || v.menu.unlockOpen || v.showRecent {
		return
	}
	stack := v.history.back
	if direction == 1 {
		stack = v.history.forward
	}
	if len(stack) == 0 {
		return
	}
	location := stack[len(stack)-1]
	source := v.currentLocation()
	v.linkJump = &linkNavigation{
		target: drawing.LinkTarget{Page: location.page, Type: drawing.DestFit},
		source: &source, restore: &location, historyDirection: direction,
	}
}

// 事件线程在阅读区布局前恢复记录；后续 resize/clamp 适配窗口变化。
func (v *viewer) applyHistoryDestination() {
	navigation := v.linkDestination
	if navigation == nil || navigation.restore == nil || !navigation.ready || v.busy {
		return
	}
	location := *navigation.restore
	v.viewport = pageViewport{
		mode: location.mode, scale: location.scale, offset: location.offset,
		viewport: location.viewport, page: location.pageSize,
	}
	c := &v.continuous
	c.enabled, c.double = location.continuous, location.double
	c.dragging, c.scrollFraction = false, 0
	c.pinch, c.touch = pinchGesture{}, touchGesture{}
	c.list.Position = layout.Position{First: location.first, Offset: location.scroll}
	c.panX, c.first = location.panX, -1
	v.page = location.page
	v.pageInput.SetText(strconv.Itoa(location.page + 1))
	v.thumbnails.reveal(location.page)
	v.scrollPage, v.scrollLanding, v.landOnPage, v.scrollOverflow = 0, 0, 0, 0
	v.pageLinks = pageLinkPointer{}
	v.linkPreview = linkPreviewState{}
	v.commitNavigation(navigation)
	v.historyRestored = true
	v.linkDestination = nil
}

func (v *viewer) handleHistoryPointer(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.history, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			return
		}
		e, ok := ev.(pointer.Event)
		if !ok || e.Source != pointer.Mouse {
			continue
		}
		previous := v.history.buttons
		v.history.buttons = e.Buttons
		if e.Kind == pointer.Cancel {
			v.history.buttons = 0
		}
		if e.Kind != pointer.Press || previous&e.Buttons != 0 {
			continue
		}
		switch e.Buttons { //nolint:exhaustive // 按键是位集，只处理单独按下的前进/后退侧键，忽略其他键及组合。
		case pointer.ButtonQuaternary:
			v.navigateHistory(-1)
		case pointer.ButtonQuinary:
			v.navigateHistory(1)
		default:
			continue
		}
		gtx.Execute(op.InvalidateCmd{})
	}
}

// 全窗口透传监听让鼠标在正文或书签侧栏上都能导航，不抢占左键、滚轮或拖动。
func (v *viewer) listenHistoryPointer(gtx layout.Context) {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	defer (pointer.PassOp{}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &v.history)
}
