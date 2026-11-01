package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
)

// drawCompositePreviewText 从矢量描述补不可见文字，位图预览不替代语义内容。
// 共用内容遍历器展开嵌套复合图元并保留裁剪，不重复绘制任何可见路径。
func (p *Document) drawCompositePreviewText(ctx DrawContext, object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox, parent *models.CTM, parentClip *geom.Path, budget *renderBudget) {
	if _, ok := ctx.(semanticTextDrawer); !ok || graphicOpacity(object.Alpha) == 0 {
		return
	}
	w, h := unit.Width, unit.Height
	ctm := models.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
	if parent != nil {
		ctm = *parent.Multiply(&ctm)
	}
	flipPage := geom.Matrix{{1, 0, 0}, {0, -1, pb.Height}}
	flipUnit := geom.Matrix{{1, 0, 0}, {0, -1, h}}
	mapping := flipPage.Mul(imagelayout.MatrixWH(object.Boundary, w, h, ctm, pb.Height)).Mul(flipUnit)
	clipCTM := models.IdentityMatrix
	if object.CTM != nil {
		clipCTM = *object.CTM
	}
	if parent != nil {
		clipCTM = *parent.Multiply(&clipCTM)
	}
	clip := p.buildImageClip(object.Clips, pb.Height, object.Boundary.X, object.Boundary.Y, clipCTM)
	clip = intersectClips(clip, parentClip)
	clip = intersectClips(clip, graphicBoundaryClip(object.Boundary, pb.Height, parent))
	if clip != nil {
		clip = clip.Copy().Transform(flipPage)
	}
	frame := contentFrame{Scope: &contentSource{Document: p, budget: budget}, Box: models.StBox{Width: w, Height: h}, Matrix: mapping, Clip: clip, DrawParam: dp}
	content.WalkItems(frame, unit.Content.Items, func(frame contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() || graphicOpacity(item.Text.Alpha) == 0 {
			return
		}
		localFlip := geom.Matrix{{1, 0, 0}, {0, -1, frame.Box.Height}}
		matrix := flipPage.Mul(frame.Matrix).Mul(localFlip)
		if !finiteMatrix(matrix) || matrix.Det() == 0 {
			return
		}
		var localClip *geom.Path
		if frame.Clip != nil {
			localClip = frame.Clip.Copy().Transform(flipPage).Transform(matrix.Inv())
		}
		text := *item.Text
		text.Alpha = nil
		text.Fill.Set(true)
		text.Stroke = false
		text.FillColor = &models.CTColor{Value: &models.Color{RGBA: geom.Black}}
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

func (p *Document) drawCompositeSemanticText(ctx DrawContext, object models.CompositeObject, items []models.PageItem, dp *models.DrawParam, pb models.StBox, w, h float64, budget *renderBudget) {
	if _, ok := ctx.(semanticTextDrawer); !ok {
		return
	}
	box := object.Boundary
	matrix := imagelayout.MatrixWH(box, w, h, models.CTM{box.Width, 0, 0, box.Height, 0, 0}, pb.Height)
	if !finiteMatrix(matrix) {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	ctx.Transform(matrix)
	var visit func([]models.PageItem)
	visit = func(items []models.PageItem) {
		for _, item := range items {
			if !pageItemPresent(item) {
				continue
			}
			switch item.Kind {
			case models.PageItemText:
				text := *item.Text
				if graphicOpacity(text.Alpha) == 0 {
					continue
				}
				text.Alpha = nil
				text.Fill.Set(true)
				text.Stroke = false
				text.FillColor = &models.CTColor{Value: &models.Color{RGBA: geom.Black}}
				p.textWithBudget(semanticOnlyContext{ctx}, text, p.objectDrawParam(text.DrawParam, dp), models.StBox{Width: w, Height: h}, nil, nil, budget)
			case models.PageItemBlock:
				visit(item.Block.Items)
			}
		}
	}
	visit(items)
}
