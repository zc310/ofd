package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
)

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
