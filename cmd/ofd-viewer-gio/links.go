package main

import (
	"image"
	"math"
	"net/url"
	"slices"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// linkedPageScene 将只读热区与页面一同缓存，不在界面线程访问 OFD 资源。
type linkedPageScene struct {
	pageScene

	links  []pageLink
	weight int64
}

func (s *linkedPageScene) Weight() int64 { return s.weight }

type pageLink struct {
	uri    string
	target *drawing.LinkTarget
	box    drawing.PageLink
	region *canvas.Path
}

func webURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "\x00\r\n\t") {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

func newLinkedPageScene(scene pageScene, links []drawing.PageLink) *linkedPageScene {
	s := &linkedPageScene{pageScene: scene}
	if weighted, ok := scene.(interface{ Weight() int64 }); ok {
		s.weight = weighted.Weight()
	}
	for _, link := range links {
		uri, ok := webURL(link.URI)
		if (!ok && !validLinkTarget(link.Target)) || !validPositive(link.Width) || !validPositive(link.Height) || !finiteGeometry(link.X) || !finiteGeometry(link.Y) {
			continue
		}
		item := pageLink{uri: uri, box: link}
		if uri == "" {
			target := *link.Target
			item.target = &target
		}
		item.box.Target = nil
		if link.Region != "" {
			region, err := canvas.ParseSVGPath(link.Region)
			if err != nil || region.Empty() {
				continue
			}
			item.region = region
		}
		s.links = append(s.links, item)
		if s.weight > 0 {
			s.weight += int64(128 + len(uri) + len(link.Region))
			if item.target != nil {
				s.weight += int64(96 + len(item.target.Name))
			}
			if item.region != nil {
				s.weight += int64(len(item.region.Data())) * 8
			}
		}
	}
	return s
}

func (l pageLink) contains(p f32.Point) bool {
	x, y := (float64(p.X)-l.box.X)/l.box.Width, (float64(p.Y)-l.box.Y)/l.box.Height
	return x >= 0 && x <= 1 && y >= 0 && y <= 1 && (l.region == nil || l.region.ContainsPoint(x, y, canvas.NonZero))
}

// pageLinkPointer 的事件坐标由 Gio 按页面变换还原为毫米，连续页无需另算列表偏移。
type pageLinkPointer struct {
	scene      pageScene
	pressed    int // 0 表示未按下，其他值为热区下标加一。
	id         pointer.ID
	start      f32.Point
	cursorTags []bool
}

func (p *pageLinkPointer) layout(gtx layout.Context, scene pageScene, scale float32, dragging bool, activate func(pageLink)) {
	if p.scene != scene {
		*p = pageLinkPointer{scene: scene}
	}
	s, ok := scene.(*linkedPageScene)
	if !ok || len(s.links) == 0 {
		return
	}
	if !gtx.Enabled() {
		p.pressed = 0
		return
	}
	// Gio 按事件所在裁剪区域的变换还原坐标，而不只看 event.Op 前的 Affine。
	// 连续页的纸张裁剪在像素空间建立，须在页面缩放后再建毫米空间裁剪。
	defer clip.Rect{Max: image.Pt(int(math.Ceil(scene.Width())), int(math.Ceil(scene.Height())))}.Push(gtx.Ops).Pop()
	for {
		e, ok := gtx.Event(pointer.Filter{Target: p, Kinds: pointer.Press | pointer.Release | pointer.Drag | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := e.(pointer.Event); ok {
			p.handle(e, s.links, scale, gtx.Metric.PxPerDp, activate)
		}
	}
	event.Op(gtx.Ops, p)
	if dragging {
		return
	}
	if len(p.cursorTags) != len(s.links) {
		p.cursorTags = make([]bool, len(s.links))
	}
	// 让 Gio 根据每帧热区位置决定光标，滚动或缩放时不依赖新的鼠标移动事件。
	// 光标使用热区外接矩形；点击仍按原始 Region 精确命中。
	for i := range s.links {
		link := &s.links[i]
		transform := op.Affine(f32.NewAffine2D(float32(link.box.Width), 0, float32(link.box.X),
			0, float32(link.box.Height), float32(link.box.Y))).Push(gtx.Ops)
		area := clip.Rect{Max: image.Pt(1, 1)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &p.cursorTags[i])
		pointer.CursorPointer.Add(gtx.Ops)
		area.Pop()
		transform.Pop()
	}
}

func (p *pageLinkPointer) handle(e pointer.Event, links []pageLink, scale, pixelsPerDp float32, activate func(pageLink)) {
	lookup := func() int {
		for i, link := range slices.Backward(links) {
			if link.contains(e.Position) {
				return i + 1
			}
		}
		return 0
	}
	switch e.Kind { //nolint:exhaustive // 滚轮由阅读区统一处理。
	case pointer.Leave:
		p.pressed = 0
	case pointer.Press:
		p.pressed = 0
		if e.Source == pointer.Mouse && e.Buttons == pointer.ButtonPrimary {
			p.pressed, p.id, p.start = lookup(), e.PointerID, e.Position
		}
	case pointer.Drag:
		if e.PointerID == p.id && linkDragExceeded(e.Position.Sub(p.start).Mul(scale), pixelsPerDp) {
			p.pressed = 0
		}
	case pointer.Release:
		if e.PointerID == p.id {
			index := p.pressed
			p.pressed = 0
			if index != 0 && lookup() == index && !linkDragExceeded(e.Position.Sub(p.start).Mul(scale), pixelsPerDp) && activate != nil {
				activate(links[index-1])
			}
		}
	case pointer.Cancel:
		p.pressed = 0
	}
}

func linkDragExceeded(delta f32.Point, pixelsPerDp float32) bool {
	return math.Hypot(float64(delta.X), float64(delta.Y)) > float64(4*max(1, pixelsPerDp))
}
