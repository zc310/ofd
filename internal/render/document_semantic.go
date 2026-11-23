package render

import (
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/pkg/spec"
)

// drawCompositePreviewText 从矢量描述补不可见文字，位图预览不替代语义内容。
// 共用内容遍历器展开嵌套复合图元并保留裁剪，不重复绘制任何可见路径。
func (p *Document) drawCompositePreviewText(ctx DrawContext, object spec.CompositeObject, unit *spec.CompositeGraphicUnit, dp *spec.DrawParam, pb spec.StBox, parent *spec.CTM, parentClip *geom.Path, budget *renderBudget) {
	if _, ok := ctx.(semanticTextDrawer); !ok || graphicOpacity(object.Alpha) == 0 {
		return
	}
	w, h := unit.Width, unit.Height
	flipUnit := geom.Matrix{{1, 0, 0}, {0, -1, h}}
	mapping := imagelayout.BoundaryMatrix(object.Boundary, w, h, parent).Mul(flipUnit)
	clipCTM := spec.IdentityCTM()
	if object.CTM != nil {
		clipCTM = *object.CTM
	}
	if parent != nil {
		clipCTM = *parent.Multiply(&clipCTM)
	}
	clip := p.buildImageClip(object.Clips, object.Boundary.X, object.Boundary.Y, clipCTM)
	clip = intersectClips(clip, parentClip)
	clip = intersectClips(clip, graphicBoundaryClip(object.Boundary, parent))
	frame := contentFrame{Scope: &contentSource{Document: p, budget: budget}, Box: spec.StBox{Width: w, Height: h}, Matrix: mapping, Clip: clip, DrawParam: dp}
	content.WalkItems(frame, unit.Content.Items, func(frame contentFrame, item spec.PageItem) {
		if item.Kind != spec.PageItemText || !item.Text.VisibleValue() || graphicOpacity(item.Text.Alpha) == 0 {
			return
		}
		matrix := frame.Matrix
		if !finiteMatrix(matrix) || matrix.Det() == 0 {
			return
		}
		var localClip *geom.Path
		if frame.Clip != nil {
			localClip = frame.Clip.Copy().Transform(matrix.Inv())
		}
		text := *item.Text
		text.Alpha = nil
		text.Fill.Set(true)
		text.Stroke = false
		text.FillColor = &spec.CTColor{Value: &spec.ColorValue{RGBA: geom.Black}}
		ctx.Push()
		defer ctx.Pop()
		ctx.Transform(matrix)
		p.textWithBudget(semanticOnlyContext{ctx}, text, p.objectDrawParam(text.DrawParam, frame.DrawParam), frame.Box, nil, localClip, budget)
	})
}

// semanticOnlyContext 复用原文字排版，只向矢量输出提交不可见文字。
// 用于半透明复合对象的独立合成，不重复输出可见内容。
type semanticOnlyContext struct{ DrawContext }

func (c semanticOnlyContext) DrawSemanticTextLine(run TextRun) {
	if drawer, ok := c.DrawContext.(semanticTextDrawer); ok {
		drawer.DrawSemanticTextLine(run)
	}
}

func (p *Document) drawCompositeSemanticText(ctx DrawContext, object spec.CompositeObject, items []spec.PageItem, dp *spec.DrawParam, pb spec.StBox, w, h float64, source spec.StBox, budget *renderBudget) {
	if _, ok := ctx.(semanticTextDrawer); !ok {
		return
	}
	box := object.Boundary
	// source、文字绘制及页面上下文均为 OFD 左上原点。
	// 与可见位图共用实测裁边，不能再按完整单元缩放，否则可搜索但高亮错位。
	matrix, ok := content.MapBox(source, box)
	if !ok {
		return
	}
	clip := geom.Rectangle(source.Width, source.Height).Translate(source.X, source.Y)
	ctx.Push()
	defer ctx.Pop()
	ctx.Transform(matrix)
	var visit func([]spec.PageItem)
	visit = func(items []spec.PageItem) {
		for _, item := range items {
			if !pageItemPresent(item) {
				continue
			}
			switch item.Kind {
			case spec.PageItemText:
				text := *item.Text
				if graphicOpacity(text.Alpha) == 0 {
					continue
				}
				text.Alpha = nil
				text.Fill.Set(true)
				text.Stroke = false
				text.FillColor = &spec.CTColor{Value: &spec.ColorValue{RGBA: geom.Black}}
				p.textWithBudget(semanticOnlyContext{ctx}, text, p.objectDrawParam(text.DrawParam, dp), spec.StBox{Width: w, Height: h}, nil, clip, budget)
			case spec.PageItemBlock:
				visit(item.Block.Items)
			}
		}
	}
	visit(items)
}
