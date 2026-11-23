package main

import (
	"image"
	"math"
	"strconv"

	"gioui.org/f32"
	"github.com/zc310/ofd/internal/render/drawing"
)

type linkNavigation struct {
	target           drawing.LinkTarget
	scale            float32
	ready            bool
	source           *navigationLocation
	restore          *navigationLocation
	historyDirection int
}

func validLinkTarget(target *drawing.LinkTarget) bool {
	if target == nil || target.Page < 0 {
		return false
	}
	for _, value := range []float64{target.Left, target.Top, target.Right, target.Bottom, target.Zoom} {
		if !finiteGeometry(value) {
			return false
		}
	}
	switch target.Type {
	case drawing.DestFit, drawing.DestFitH, drawing.DestFitV:
		return true
	case drawing.DestXYZ:
		return target.Zoom == 0 || (target.Zoom >= 0.1 && target.Zoom <= 64)
	case drawing.DestFitR:
		return target.Right > target.Left && target.Bottom > target.Top
	default:
		return false
	}
}

func (v *viewer) activatePageLink(link pageLink) {
	if v.busy {
		return
	}
	if link.target != nil {
		// 页面布局期间只记录意图，下一帧再提交后台切页，避免重放时更换文档。
		source := v.currentLocation()
		v.linkJump = &linkNavigation{target: *link.target, scale: v.viewport.scale, source: &source}
	} else if v.openURL != nil {
		v.openURL(link.uri)
	}
}

func (v *viewer) processLinkJump(submit func(request)) {
	navigation := v.linkJump
	if navigation == nil || v.busy {
		return
	}
	v.linkJump = nil
	v.linkDestination = nil
	if !validLinkTarget(&navigation.target) || navigation.target.Page >= v.total {
		v.status = "链接跳转目标无效"
		return
	}
	v.scrollPage, v.scrollLanding, v.landOnPage, v.scrollOverflow = 0, 0, 0, 0
	v.linkDestination = navigation
	if navigation.restore != nil {
		switch {
		case navigation.restore.continuous && v.continuous.metadata != nil:
			navigation.ready = true
		case !v.continuous.enabled && navigation.target.Page == v.page:
			navigation.ready = true
		default:
			submit(request{page: navigation.target.Page, forcePage: true})
		}
		return
	}
	switch {
	case v.continuous.enabled && v.continuous.metadata != nil:
		navigation.ready = true
		submit(request{page: navigation.target.Page})
	case navigation.target.Page == v.page:
		navigation.ready = true
	default:
		submit(request{page: navigation.target.Page})
	}
}

// linkTargetViewport 按 GB/T 33190—2016 §14.2 表54（PDF 第80页）计算目标视图。
// 坐标从页顶向下；XYZ 的 Zoom 为0时保留点击前的实际显示比例，而非切页后的自适应比例。
func linkTargetViewport(target drawing.LinkTarget, size image.Point, page f32.Point, previousScale, pixelsPerDp float32) (pageViewport, bool) {
	v := pageViewport{mode: fitManual, viewport: size, page: page, scale: previousScale}
	if !validLinkTarget(&target) || size.X <= 0 || size.Y <= 0 || !validPositive(float64(page.X)) || !validPositive(float64(page.Y)) {
		return v, false
	}
	w, h := float64(size.X), float64(size.Y)
	x, y := target.Left, target.Top
	scale := float64(previousScale)
	switch target.Type {
	case drawing.DestFit:
		scale, x, y = min(w/float64(page.X), h/float64(page.Y)), 0, 0
	case drawing.DestFitH:
		scale, x = w/float64(page.X), 0
	case drawing.DestFitV:
		scale, y = h/float64(page.Y), 0
	case drawing.DestXYZ:
		if target.Zoom != 0 {
			scale = 96 / 25.4 * float64(max(1, pixelsPerDp)) * target.Zoom
		}
	case drawing.DestFitR:
		scale = min(w/(target.Right-x), h/(target.Bottom-y))
		x -= (w/scale - (target.Right - x)) / 2
		y -= (h/scale - (target.Bottom - y)) / 2
	}
	if !validPositive(scale) || scale*float64(page.X) > 1e6 || scale*float64(page.Y) > 1e6 {
		return v, false
	}
	v.scale = float32(scale)
	v.offset = f32.Pt(float32(-x*scale), float32(-y*scale))
	v.clamp()
	return v, true
}

func (v *viewer) applySingleLinkDestination(size image.Point, pixelsPerDp float32) {
	navigation := v.linkDestination
	if navigation == nil || !navigation.ready || v.busy || navigation.target.Page != v.page {
		return
	}
	v.linkDestination = nil
	viewport, ok := linkTargetViewport(navigation.target, size, v.viewport.page, navigation.scale, pixelsPerDp)
	if !ok {
		v.status = "链接跳转目标无效"
		return
	}
	v.viewport = viewport
	v.landOnPage = 0
	v.commitNavigation(navigation)
}

func (v *viewer) applyContinuousLinkDestination(size image.Point, pixelsPerDp float32) {
	navigation := v.linkDestination
	if navigation == nil || !navigation.ready || v.busy {
		return
	}
	c := &v.continuous
	target := navigation.target.Page
	if target < 0 || target >= len(c.metadata) {
		v.linkDestination = nil
		v.status = "链接跳转目标无效"
		return
	}
	meta := c.metadata[target]
	viewport, ok := linkTargetViewport(navigation.target, size, f32.Pt(float32(meta.size.width), float32(meta.size.height)), navigation.scale, pixelsPerDp)
	if !ok || meta.err != nil {
		v.linkDestination = nil
		v.status = "链接跳转目标无效"
		return
	}
	first, end := target, target+1
	if c.double {
		first = target / 2 * 2
		end = min(first+2, len(c.metadata))
	}
	gap := float32(math.Round(float64(12 * max(1, pixelsPerDp))))
	var width, targetX float32
	for page := first; page < end; page++ {
		if page == target {
			targetX = width
		}
		width += max(1, float32(math.Round(c.metadata[page].size.width*float64(viewport.scale))))
		if page+1 < end {
			width += gap
		}
	}
	c.jump(target)
	c.contentWidth = width
	c.panX = max(min(0, float32(size.X)-width), min(0, viewport.offset.X-targetX))
	c.list.Position.Offset = int(math.Round(float64(-viewport.offset.Y)))
	v.viewport.mode, v.viewport.scale = fitManual, viewport.scale
	v.page = target
	v.pageInput.SetText(strconv.Itoa(target + 1))
	v.thumbnails.reveal(target)
	v.commitNavigation(navigation)
	v.linkDestination = nil
}
