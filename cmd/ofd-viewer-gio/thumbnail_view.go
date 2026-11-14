package main

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/cache"
)

const (
	thumbnailCacheEntries = 96
	thumbnailCacheBytes   = 8 << 20
)

type thumbnailImage struct {
	image *image.RGBA
	op    paint.ImageOp
}

type thumbnailState struct {
	open       bool
	busy       bool
	generation uint64
	filename   string
	next       int
	list       widget.List
	buttons    map[int]*widget.Clickable
	images     *cache.LRU[int, thumbnailImage]
	failures   *cache.LRU[int, bool]
	paper      color.NRGBA
}

func (s *thumbnailState) reset(filename string) {
	s.generation++
	s.filename = filename
	s.next = -1
	s.list = widget.List{}
	s.buttons = nil
	s.images = cache.NewWeightedLRU[int, thumbnailImage](thumbnailCacheEntries, thumbnailCacheBytes, nil)
	s.failures = cache.NewLRU[int, bool](thumbnailCacheEntries, nil)
}

func (s *thumbnailState) reveal(page int) {
	s.list.Position.First = page
	s.list.Position.Offset = 0
}

func (s *thumbnailState) accept(result thumbnailResult) {
	s.busy = false
	if result.generation != s.generation || s.images == nil || s.failures == nil {
		return
	}
	if result.err != nil || result.image == nil || result.image.Bounds().Empty() {
		s.failures.Add(result.page, true)
		return
	}
	weight := int64(len(result.image.Pix))
	if weight <= 0 || weight > thumbnailCacheBytes {
		s.failures.Add(result.page, true)
		return
	}
	s.images.AddWeighted(result.page, thumbnailImage{image: result.image, op: paint.NewImageOp(result.image)}, weight)
}

func (s *thumbnailState) handleClicks(gtx layout.Context, current int, busy bool, submit func(request)) {
	for page, button := range s.buttons {
		if button.Clicked(gtx) && !busy && page != current {
			submit(request{page: page})
			return
		}
	}
}

// 列表仅为当前可见项保留按钮，并在布局时选出一个待渲染项；不在布局中执行渲染。
func (s *thumbnailState) layout(gtx layout.Context, th *material.Theme, total, current int, disabled bool) layout.Dimensions {
	if disabled {
		gtx = gtx.Disabled()
	}
	s.list.Axis = layout.Vertical
	s.next = -1
	visible := make(map[int]*widget.Clickable)
	dims := material.List(th, &s.list).Layout(gtx, total, func(gtx layout.Context, page int) layout.Dimensions {
		button := s.buttons[page]
		if button == nil {
			button = new(widget.Clickable)
		}
		visible[page] = button
		var entry thumbnailImage
		var present, failed bool
		if s.images != nil {
			entry, present = s.images.Get(page)
			_, failed = s.failures.Get(page)
		}
		if !present && !failed && s.next < 0 && !s.busy && !disabled {
			s.next = page
		}
		return material.Clickable(gtx, button, func(gtx layout.Context) layout.Dimensions {
			semantic.DescriptionOp("第 " + strconv.Itoa(page+1) + " 页").Add(gtx.Ops)
			width := gtx.Constraints.Max.X
			height := min(gtx.Constraints.Max.Y, gtx.Dp(unit.Dp(160)))
			gtx.Constraints = layout.Exact(image.Pt(width, height))
			background := color.NRGBA{R: 210, G: 214, B: 220, A: 255}
			if page == current {
				background = color.NRGBA{R: 165, G: 194, B: 229, A: 255}
			}
			paint.FillShape(gtx.Ops, background, clip.Rect{Max: image.Pt(width, height)}.Op())
			return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if present {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						imageSize := entry.image.Bounds().Size()
						scale := min(float64(gtx.Constraints.Max.X)/float64(imageSize.X), float64(gtx.Constraints.Max.Y)/float64(imageSize.Y))
						size := image.Pt(max(1, int(float64(imageSize.X)*scale)), max(1, int(float64(imageSize.Y)*scale)))
						gtx.Constraints = layout.Exact(size)
						paint.FillShape(gtx.Ops, s.paper, clip.Rect{Max: size}.Op())
						return thumbnailWithPageBadge(gtx, th, page, func(gtx layout.Context) layout.Dimensions {
							return (widget.Image{Src: entry.op, Fit: widget.Contain, Position: layout.Center}).Layout(gtx)
						})
					})
				}
				label := "加载中…"
				if failed {
					label = "无法生成"
				}
				return thumbnailWithPageBadge(gtx, th, page, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, material.Caption(th, label).Layout)
				})
			})
		})
	})
	s.buttons = visible
	return dims
}

// 页码覆盖在图片右下角，背景增强深色或透明纸张上的可读性。
func thumbnailWithPageBadge(gtx layout.Context, th *material.Theme, page int, content layout.Widget) layout.Dimensions {
	return layout.Stack{Alignment: layout.SE}.Layout(gtx,
		layout.Expanded(content),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						paint.FillShape(gtx.Ops, color.NRGBA{R: 20, G: 24, B: 30, A: 210}, clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(unit.Dp(3))).Op(gtx.Ops))
						return layout.Dimensions{Size: gtx.Constraints.Min}
					},
					func(gtx layout.Context) layout.Dimensions {
						label := material.Caption(th, strconv.Itoa(page+1))
						label.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						label.MaxLines = 1
						return layout.Inset{Left: unit.Dp(5), Right: unit.Dp(5), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, label.Layout)
					})
			})
		}))
}
