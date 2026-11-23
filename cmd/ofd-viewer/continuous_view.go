package main

import (
	"errors"
	"image"
	"math"
	"strconv"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/cache"
)

const (
	continuousMetadataRequest = 1
	continuousPagesRequest    = 2
)

type continuousEntry struct {
	scene     pageScene
	recording sceneRecording
	links     pageLinkPointer
}

type continuousView struct {
	enabled        bool
	double         bool
	metadata       []continuousPageMetadata
	metadataFailed bool
	list           widget.List
	entries        *cache.LRU[int, *continuousEntry]
	failures       *cache.LRU[int, bool]
	next           []int
	visible        []int
	blocked        map[int]bool
	first          int
	pending        int
	panX           float32
	dragging       bool
	pointerID      pointer.ID
	lastPointer    f32.Point
	dragStart      f32.Point
	scrollFraction float64
	contentWidth   float32
	pinch          pinchGesture
	touch          touchGesture
}

func (v *viewer) zoomViewport(factor float32, anchor f32.Point, pixelsPerDp float32) {
	if v.continuous.enabled {
		v.continuous.zoom(&v.viewport, factor, anchor, pixelsPerDp)
	} else {
		v.viewport.zoom(factor, anchor, pixelsPerDp)
	}
}

func (v *viewer) toggleContinuous(submit func(request)) {
	v.continuous.enabled = !v.continuous.enabled
	if !v.continuous.enabled {
		submit(request{page: v.page})
		return
	}
	if v.continuous.entries == nil {
		v.continuous.reset(v.page)
	}
	v.continuous.jump(v.page)
}

func (v *viewer) scheduleContinuous(submit func(request)) {
	c := &v.continuous
	if !c.enabled || v.busy || v.scene == nil || v.linkJump != nil {
		return
	}
	if c.metadata == nil && !c.metadataFailed {
		c.pending = continuousMetadataRequest
		submit(request{metadata: true})
	} else if len(c.next) > 0 && !c.pinch.fingers[0].active && !c.pinch.fingers[1].active {
		// 页面批量加载会禁用阅读区输入；触摸期间先保持手势连续，松手后再补齐页面。
		c.pending = continuousPagesRequest
		submit(request{pages: append([]int(nil), c.next...)})
	}
}

func (c *continuousView) reset(page int) {
	c.metadata = nil
	c.metadataFailed = false
	c.list = widget.List{}
	c.jump(page)
	c.entries = cache.NewWeightedLRU[int, *continuousEntry](pageSceneCacheEntries, pageSceneCacheBytes, nil)
	c.failures = cache.NewLRU[int, bool](pageSceneCacheEntries*4, nil)
	c.next = nil
	c.visible = nil
	c.blocked = make(map[int]bool)
	c.first = -1
	c.panX, c.dragging = 0, false
	c.scrollFraction, c.contentWidth = 0, 0
	c.pinch = pinchGesture{}
	c.touch = touchGesture{}
}

func (c *continuousView) jump(page int) {
	if c.double {
		page /= 2
	}
	c.list.Position.First = page
	c.list.Position.Offset = 0
	c.scrollFraction = 0
}

func (c *continuousView) pageX(width, viewport float32) float32 {
	if width <= viewport {
		return (viewport - width) / 2
	}
	return max(viewport-width, min(0, c.panX))
}

func (c *continuousView) panHorizontal(delta, viewport float32) {
	if !finiteGeometry(float64(delta)) {
		return
	}
	c.panX = max(min(0, viewport-c.contentWidth), min(0, c.panX+delta))
}

func (c *continuousView) scrollVertical(delta float32) {
	if !finiteGeometry(float64(delta)) {
		return
	}
	value := max(-1e6, min(1e6, float64(c.list.Position.Offset)+float64(delta)+c.scrollFraction))
	whole := math.Trunc(value)
	c.list.Position.Offset = int(whole)
	c.scrollFraction = value - whole
}

// zoom 保持当前首个可见页面内的鼠标锚点；页间距固定时跨页锚点为近似值。
func (c *continuousView) zoom(v *pageViewport, factor float32, anchor f32.Point, pixelsPerDp float32) {
	old := v.scale
	if old <= 0 || factor <= 0 || !finiteGeometry(float64(factor)) {
		return
	}
	oldX := c.pageX(v.page.X*old, float32(v.viewport.X))
	v.zoom(factor, anchor, pixelsPerDp)
	ratio := v.scale / old
	if ratio == 1 {
		return
	}
	c.panX = anchor.X - (anchor.X-oldX)*ratio
	c.panX = max(min(0, float32(v.viewport.X)-v.page.X*v.scale), min(0, c.panX))
	offset := (float64(c.list.Position.Offset)+float64(anchor.Y))*float64(ratio) - float64(anchor.Y)
	c.list.Position.Offset = int(max(-1e6, min(1e6, math.Round(offset))))
	c.scrollFraction = 0
}

func (c *continuousView) handlePointer(gtx layout.Context, viewport *pageViewport, doubleTap func(f32.Point, float32)) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: c,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1e6, Max: 1e6}, ScrollY: pointer.ScrollRange{Min: -1e6, Max: 1e6}})
		if !ok {
			break
		}
		p, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		factor, anchor, delta, grab := c.pinch.handle(p)
		if c.touch.handle(p, gtx.Metric.PxPerDp, grab || c.pinch.active()) {
			doubleTap(p.Position, gtx.Metric.PxPerDp)
		}
		if grab {
			c.dragging = false
			for _, finger := range c.pinch.fingers {
				gtx.Execute(pointer.GrabCmd{Tag: c, ID: finger.id})
			}
		}
		if factor > 0 {
			c.zoom(viewport, factor, anchor, gtx.Metric.PxPerDp)
			c.panHorizontal(delta.X, float32(viewport.viewport.X))
			c.scrollVertical(-delta.Y)
		}
		switch p.Kind { //nolint:exhaustive // 只处理阅读区滚动和拖动。
		case pointer.Scroll:
			if p.Modifiers&key.ModCtrl != 0 {
				factor := float32(math.Exp(float64(-p.Scroll.Y/max(gtx.Metric.PxPerDp, 1)) / 300))
				c.zoom(viewport, factor, p.Position, gtx.Metric.PxPerDp)
			} else {
				c.panHorizontal(-p.Scroll.X, float32(viewport.viewport.X))
				c.scrollVertical(p.Scroll.Y)
			}
		case pointer.Press:
			if (p.Source == pointer.Touch && !c.pinch.active()) || (p.Source == pointer.Mouse && p.Buttons&pointer.ButtonPrimary != 0) {
				c.dragging, c.pointerID, c.lastPointer = true, p.PointerID, p.Position
				c.dragStart = p.Position
			}
		case pointer.Drag:
			if c.dragging && p.PointerID == c.pointerID && (p.Source != pointer.Touch || !c.pinch.active()) {
				delta := p.Position.Sub(c.lastPointer)
				c.panHorizontal(delta.X, float32(viewport.viewport.X))
				c.scrollVertical(-delta.Y)
				c.lastPointer = p.Position
				if linkDragExceeded(p.Position.Sub(c.dragStart), gtx.Metric.PxPerDp) {
					gtx.Execute(pointer.GrabCmd{Tag: c, ID: p.PointerID})
				}
			}
		case pointer.Release:
			if p.PointerID == c.pointerID {
				c.dragging = false
			}
		case pointer.Cancel:
			c.dragging = false
		}
		gtx.Execute(op.InvalidateCmd{})
	}
}

func (c *continuousView) accept(response result) {
	if response.err != nil {
		if c.pending == continuousMetadataRequest {
			c.metadataFailed = true
		}
		c.pending = 0
		return
	}
	if c.pending == continuousMetadataRequest {
		c.metadata = response.metadata
	} else if c.pending == continuousPagesRequest {
		var visibleWeight int64
		for _, page := range c.visible {
			if entry, ok := c.entries.Get(page); ok {
				if weighted, ok := entry.scene.(interface{ Weight() int64 }); ok {
					visibleWeight += weighted.Weight()
				}
			}
		}
		for _, page := range response.pages {
			if page.err != nil || page.scene == nil {
				if errors.Is(page.err, errBatchWeight) {
					c.blocked[page.page] = true
				} else {
					c.failures.Add(page.page, true)
				}
				continue
			}
			weighted, ok := page.scene.(interface{ Weight() int64 })
			if !ok || weighted.Weight() <= 0 || weighted.Weight() > pageSceneCacheBytes {
				c.failures.Add(page.page, true)
				continue
			}
			if weighted.Weight() > pageSceneCacheBytes-visibleWeight {
				c.blocked[page.page] = true
				continue
			}
			visibleWeight += weighted.Weight()
			c.entries.AddWeighted(page.page, &continuousEntry{scene: page.scene}, weighted.Weight())
		}
	}
	c.pending = 0
}

// layout 使用 Gio 虚拟列表，只重放前四个可见页面；滚动期间不遍历整篇文档。
func (v *viewer) layoutContinuous(gtx layout.Context, th *material.Theme) layout.Dimensions {
	c := &v.continuous
	size := gtx.Constraints.Max
	if v.busy {
		gtx = gtx.Disabled()
		c.dragging = false
		c.pinch = pinchGesture{}
		c.touch = touchGesture{}
	}
	if c.metadata == nil {
		label := "正在读取页面尺寸…"
		if c.metadataFailed {
			label = "无法读取页面尺寸"
		}
		return layout.Center.Layout(gtx, material.Body1(th, label).Layout)
	}
	v.applyContinuousLinkDestination(size, gtx.Metric.PxPerDp)
	c.handlePointer(gtx, &v.viewport, v.doubleTapZoom)
	if v.viewport.mode != fitManual {
		c.panX = 0
	}
	c.next = c.next[:0]
	c.visible = c.visible[:0]
	c.contentWidth = 0
	c.list.Axis = layout.Vertical
	visibleCount := 0
	columns := 1
	if c.double {
		columns = 2
	}
	rows := (len(c.metadata) + columns - 1) / columns
	dims := material.List(th, &c.list).Layout(gtx, rows, func(gtx layout.Context, row int) layout.Dimensions {
		first := row * columns
		end := min(first+columns, len(c.metadata))
		var rowWidth, rowHeight float64
		for page := first; page < end; page++ {
			rowWidth += c.metadata[page].size.width
			rowHeight = max(rowHeight, c.metadata[page].size.height)
		}
		gap := gtx.Dp(unit.Dp(12))
		innerGap := gap * (end - first - 1)
		width := max(1, size.X-gtx.Dp(unit.Dp(24)))
		height := max(1, size.Y-gtx.Dp(unit.Dp(24)))
		scale := float64(max(1, width-innerGap)) / rowWidth
		if v.viewport.mode == fitWhole {
			scale = min(scale, float64(height)/rowHeight)
		} else if v.viewport.mode == fitManual && v.viewport.scale > 0 {
			scale = float64(v.viewport.scale)
		}
		// 防止不可信尺寸导致列表整数溢出或无法分配的裁剪区域。
		if !validPositive(scale) || scale*rowHeight > 1e6 || scale*rowWidth > 1e6 {
			return layout.Dimensions{Size: image.Pt(size.X, min(height, 100))}
		}
		if v.page >= first && v.page < end {
			previousScale := v.viewport.scale
			v.viewport.viewport = size
			v.viewport.page = f32.Pt(float32(rowWidth)+float32(innerGap)/float32(scale), float32(rowHeight))
			v.viewport.scale = float32(scale)
			if previousScale != v.viewport.scale {
				gtx.Execute(op.InvalidateCmd{})
			}
		}
		pixelWidth := innerGap
		for page := first; page < end; page++ {
			pixelWidth += max(1, int(math.Round(c.metadata[page].size.width*scale)))
		}
		c.contentWidth = max(c.contentWidth, float32(pixelWidth))
		itemHeight := max(1, int(math.Round(rowHeight*scale))) + gap
		x := int(math.Round(float64(c.pageX(float32(pixelWidth), float32(size.X)))))
		for page := first; page < end; page++ {
			visibleCount++
			if visibleCount <= pageSceneCacheEntries {
				c.visible = append(c.visible, page)
			}
			meta := c.metadata[page]
			pageWidth := max(1, int(math.Round(meta.size.width*scale)))
			pageHeight := max(1, int(math.Round(meta.size.height*scale)))
			transform := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
			paper := clip.Rect{Max: image.Pt(pageWidth, pageHeight)}.Push(gtx.Ops)
			pointer.CursorGrab.Add(gtx.Ops)
			paint.Fill(gtx.Ops, paperColor(v.background, v.backgroundCustom))
			entry, present := c.entries.Get(page)
			_, failed := c.failures.Get(page)
			if meta.err == nil && !present && !failed && !c.blocked[page] && visibleCount <= pageSceneCacheEntries {
				c.next = append(c.next, page)
			}
			drawn := false
			if present {
				transformScale := op.Affine(f32.NewAffine2D(float32(scale), 0, 0, 0, float32(scale), 0)).Push(gtx.Ops)
				if v.busy {
					drawn = entry.recording.drawCached(gtx, entry.scene, float32(scale))
				} else if c.pinch.active() && entry.recording.scene == entry.scene && entry.recording.nativeLinear {
					// Gio 的渐变色轴依赖缩放。捏合时暂用旧色轴预览，避免每帧重建整页；
					// 松手后 draw 会按实际比例重建，恢复精确颜色。
					entry.recording.call.Add(gtx.Ops)
					drawn = true
				} else {
					entry.recording.draw(gtx, entry.scene, float32(scale))
					drawn = true
				}
				v.layoutPageLinks(gtx, &entry.links, entry.scene, float32(scale), c.dragging)
				transformScale.Pop()
			}
			if !drawn {
				label := "正在加载…"
				switch {
				case meta.err != nil:
					label = "页面尺寸无效"
				case failed:
					label = "页面加载失败"
				case c.blocked[page]:
					label = "页面超出缓存预算"
				}
				labelContext := gtx
				labelContext.Constraints = layout.Exact(image.Pt(pageWidth, pageHeight))
				layout.Center.Layout(labelContext, material.Caption(th, label).Layout)
			}
			paper.Pop()
			transform.Pop()
			x += pageWidth + gap
		}
		return layout.Dimensions{Size: image.Pt(size.X, itemHeight)}
	})
	// 右侧留给列表滚动条；其余阅读区自行处理滚轮和拖动，避免 Ctrl 滚轮同时滚页。
	area := clip.Rect{Max: image.Pt(max(0, size.X-gtx.Dp(unit.Dp(14))), size.Y)}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, c)
	pass.Pop()
	// 普通状态使用下层页面热区的光标，穿透事件层不能覆盖链接手形。
	if c.dragging {
		pointer.CursorGrabbing.Add(gtx.Ops)
	}
	area.Pop()
	if c.list.Position.First != c.first {
		clear(c.blocked)
		c.first = c.list.Position.First
		gtx.Execute(op.InvalidateCmd{})
	}
	if c.list.Position.First >= 0 && c.list.Position.First < rows && c.list.Position.First != v.page/columns {
		v.page = c.list.Position.First * columns
		v.pageInput.SetText(strconv.Itoa(v.page + 1))
		v.thumbnails.reveal(v.page)
		gtx.Execute(op.InvalidateCmd{})
	}
	return dims
}
