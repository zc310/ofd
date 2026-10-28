package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	patternlayout "github.com/zc310/ofd/internal/render/layout/pattern"
	"github.com/zc310/ofd/internal/render/renderstate"
)

// drawPatternPath 按平铺计划直接绘制 CellContent 矢量对象。
func (p *Document) drawPatternPath(ctx DrawContext, path *geom.Path, pattern *models.CtPattern, object models.PathObject, pb models.StBox, parentCTM *models.CTM, budget *renderBudget) bool {
	if pattern == nil || path == nil || len(pattern.CellContent.Items) == 0 {
		return false
	}
	plan, status := patternlayout.New(pattern, object.CTGraphicUnit, pb, path.FastBounds(), parentCTM)
	if status == patternlayout.LimitExceeded {
		budget.ReportLimit(renderstate.LimitPatternTiles, "pattern_tiles", "columns", plan.Columns(), "rows", plan.Rows(), "limit", patternlayout.MaxTiles)
	}
	if status != patternlayout.Ready || !budget.AllowPatternTiles(plan.Columns()*plan.Rows()) {
		return false
	}
	for column := range plan.Columns() {
		for row := range plan.Rows() {
			tile, ok := plan.Tile(column, row)
			if !ok {
				return false
			}
			p.drawItemsWithTransform(ctx, pattern.CellContent.Items, nil, pb, &tile, path, 0, budget)
		}
	}
	return true
}
