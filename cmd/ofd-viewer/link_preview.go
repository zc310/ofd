package main

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/render/drawing"
)

const (
	linkPreviewDelay        = 400 * time.Millisecond
	linkPreviewMaxPixels    = 2048
	linkPreviewCacheEntries = 4
	linkPreviewCacheBytes   = int64(32 << 20)
)

// 悬停状态只由事件线程更新；每帧重新收集可见热区，避免虚拟列表回收后残留预览。
type linkPreviewState struct {
	mouse    bool
	position f32.Point
	source   *pageLinkPointer
	index    int
	target   drawing.LinkTarget
	anchor   image.Rectangle
	since    time.Time
	active   bool
	visible  bool
}

func (s *linkPreviewState) update(gtx layout.Context) {
	s.active, s.visible = false, false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: s, Kinds: pointer.Enter | pointer.Move | pointer.Leave | pointer.Press | pointer.Drag | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			s.mouse = e.Source == pointer.Mouse && (e.Kind == pointer.Enter || e.Kind == pointer.Move)
			s.position = e.Position
		}
	}
}

func (v *viewer) layoutPageLinks(gtx layout.Context, p *pageLinkPointer, scene pageScene, scale float32, dragging bool) {
	p.layout(gtx, scene, scale, dragging, v.activatePageLink)
	s := &v.linkPreview
	if !s.mouse || v.busy || dragging || p.hovered == 0 || !gtx.Enabled() {
		return
	}
	linked, ok := scene.(*linkedPageScene)
	if !ok || p.hovered > len(linked.links) {
		return
	}
	link := linked.links[p.hovered-1]
	if !validLinkTarget(link.target) || link.target.Page >= v.total {
		return
	}
	if s.source != p || s.index != p.hovered || s.target != *link.target {
		s.since = gtx.Now
	}
	s.source, s.index, s.target, s.active = p, p.hovered, *link.target, true
	// 根坐标鼠标位置减去热区内偏移，得到不受页面缩放影响的浮层锚点。
	position := p.cursorTags[p.hovered-1].position
	width, height := float32(link.box.Width)*scale, float32(link.box.Height)*scale
	left, top := s.position.X-position.X*width, s.position.Y-position.Y*height
	s.anchor = image.Rect(int(math.Round(float64(left))), int(math.Round(float64(top))),
		int(math.Round(float64(left+width))), int(math.Round(float64(top+height))))
}

func (v *viewer) layoutLinkPreview(gtx layout.Context, th *material.Theme) {
	s := &v.linkPreview
	if !s.active || v.busy || v.menu.open || v.menu.about || v.showRecent {
		s.source = nil
		return
	}
	ready := s.since.Add(linkPreviewDelay)
	if gtx.Now.Before(ready) {
		gtx.Execute(op.InvalidateCmd{At: ready})
		return
	}
	s.visible = true
	frame := linkPreviewFrame(gtx.Constraints.Max, s.anchor, max(1, gtx.Metric.PxPerDp))
	if frame.Empty() {
		return
	}
	defer op.Offset(frame.Min).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(frame.Size())
	paintLinkPreviewShadow(gtx)
	radius := gtx.Dp(unit.Dp(7))
	defer clip.UniformRRect(image.Rectangle{Max: frame.Size()}, radius).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, color.NRGBA{R: 130, G: 143, B: 162, A: 255})
	layout.UniformInset(unit.Dp(1)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		// 内外圆角保持同心，底板和图片都不能覆盖转角处的灰色边框。
		innerRadius := max(0, radius-gtx.Dp(unit.Dp(1)))
		defer clip.UniformRRect(image.Rectangle{Max: size}, innerRadius).Push(gtx.Ops).Pop()
		// 透明纸张仍需不透明底板，避免预览内容与下层正文重叠。
		paint.Fill(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		paint.Fill(gtx.Ops, paperColor(v.background, v.backgroundCustom))
		return linkPreviewWithPageBadge(gtx, th, s.target.Page, func(gtx layout.Context) layout.Dimensions {
			if v.thumbnails.previews != nil {
				if entry, ok := v.thumbnails.previews.Get(s.target.Page); ok {
					offset, imageSize := linkPreviewCrop(size, entry.image.Bounds().Size(), s.target)
					defer op.Offset(offset).Push(gtx.Ops).Pop()
					gtx.Constraints = layout.Exact(imageSize)
					return (widget.Image{Src: entry.op, Fit: widget.Fill}).Layout(gtx)
				}
			}
			label := "加载中…"
			if v.thumbnails.previewFailures != nil {
				if _, failed := v.thumbnails.previewFailures.Get(s.target.Page); failed {
					label = "无法生成预览"
				}
			}
			return layout.Center.Layout(gtx, material.Body2(th, label).Layout)
		})
	})
}

// 页码固定在浮层右下角，不跟随目标图片的局部裁剪，也不注册交互热区。
func linkPreviewWithPageBadge(gtx layout.Context, th *material.Theme, page int, content layout.Widget) layout.Dimensions {
	size := gtx.Constraints.Max
	text := "第 " + strconv.Itoa(page+1) + " 页"
	return layout.Stack{Alignment: layout.SE}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(size)
			content(gtx)
			return layout.Dimensions{Size: size}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(unit.Dp(4))).Push(gtx.Ops).Pop()
						semantic.DescriptionOp(text).Add(gtx.Ops)
						paint.Fill(gtx.Ops, color.NRGBA{R: 20, G: 24, B: 30, A: 210})
						return layout.Dimensions{Size: gtx.Constraints.Min}
					},
					func(gtx layout.Context) layout.Dimensions {
						label := material.Caption(th, text)
						label.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						label.MaxLines = 1
						return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, label.Layout)
					})
			})
		}))
}

// 多层低透明度圆角扩散形成柔和外阴影；在内容裁剪前绘制，避免阴影被卡片边缘截掉。
func paintLinkPreviewShadow(gtx layout.Context) {
	offset := image.Pt(0, gtx.Dp(unit.Dp(5)))
	bounds := image.Rectangle{Max: gtx.Constraints.Max}.Add(offset)
	for spread := 12; spread > 0; spread-- {
		radius := gtx.Dp(unit.Dp(spread))
		paint.FillShape(gtx.Ops, color.NRGBA{R: 23, G: 32, B: 51, A: uint8(3 + (12-spread)/2)},
			clip.UniformRRect(bounds.Inset(-radius), gtx.Dp(unit.Dp(7+spread))).Op(gtx.Ops))
	}
}

// 浮层尺寸和摆放沿用 WASM：宽至多820 dp、高至多420 dp或窗口45%，优先放在链接上方。
func linkPreviewFrame(window image.Point, anchor image.Rectangle, scale float32) image.Rectangle {
	margin := int(math.Round(float64(12 * scale)))
	size := image.Pt(min(int(820*scale), window.X-2*margin), min(int(420*scale), window.Y*45/100))
	if size.X <= 0 || size.Y <= 0 {
		return image.Rectangle{}
	}
	left := max(margin, min((anchor.Min.X+anchor.Max.X-size.X)/2, window.X-size.X-margin))
	top := anchor.Min.Y - size.Y - margin
	if top < margin {
		top = max(margin, min(anchor.Max.Y+margin, window.Y-size.Y-margin))
	}
	return image.Rectangle{Min: image.Pt(left, top), Max: image.Pt(left+size.X, top+size.Y)}
}

// 按目标位置截取局部，目标尽量位于预览高度的30%；无位置的 Fit 预览页面中部。
func linkPreviewCrop(frame, page image.Point, target drawing.LinkTarget) (image.Point, image.Point) {
	height := max(1, int(math.Round(float64(frame.X)*float64(page.Y)/float64(max(1, page.X)))))
	ratio := 0.5
	if target.Type != drawing.DestFit && validPositive(target.PageHeight) {
		top := target.Top
		if target.Type == drawing.DestFitV {
			top = 0
		}
		ratio = max(0, min(1, top/target.PageHeight))
	}
	top := int(math.Round(float64(frame.Y)*0.3 - ratio*float64(height)))
	top = max(min(0, frame.Y-height), min(0, top))
	return image.Pt(0, top), image.Pt(frame.X, height)
}

// 预览与侧栏共用串行 worker，但使用独立的清晰位图缓存，预览请求优先。
func (v *viewer) nextThumbnailRequest() (thumbnailRequest, bool) {
	s := &v.thumbnails
	if v.busy || s.busy || s.filename == "" {
		return thumbnailRequest{}, false
	}
	page, preview := -1, false
	if v.linkPreview.visible && s.previews != nil && s.previewFailures != nil {
		target := v.linkPreview.target.Page
		_, present := s.previews.Get(target)
		_, failed := s.previewFailures.Get(target)
		if !present && !failed {
			page, preview = target, true
		}
	}
	if page < 0 && s.open {
		page = s.next
	}
	if page < 0 {
		return thumbnailRequest{}, false
	}
	var session *continuousSession
	if v.sceneLease != nil {
		session = v.sceneLease.session
	}
	return thumbnailRequest{filename: s.filename, generation: s.generation, page: page, preview: preview, session: session}, true
}

// 根级透传监听只记录鼠标，不抢占链接点击、滚动或拖动，也不让浮层成为热区。
func (s *linkPreviewState) listen(gtx layout.Context) {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	defer (pointer.PassOp{}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, s)
}
