package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	patternlayout "github.com/zc310/ofd/internal/render/layout/pattern"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/internal/render/renderstate"
)

// patternFallbackColor 只在绘制失败时使用：优先保留基本颜色，没有基本颜色则用黑色。
// 创建副本而不修改资源，保留颜色 Alpha 供普通绘制路径与对象 Alpha 相乘。
func patternFallbackColor(source *models.CTColor) *models.CTColor {
	fallback := *source
	fallback.Pattern = nil
	if !paint.HasColorValue(&fallback) {
		fallback.Value = &models.Color{RGBA: geom.Black}
	}
	return &fallback
}

// drawPatternPaint 按 GB/T 33190—2016 §8.3 表26/28、§8.4 表34，先绘制整幅
// 底纹再统一合成颜色和图元 Alpha，避免重叠的单元或子图元重复叠加外层透明度。
func (p *Document) drawPatternPaint(ctx DrawContext, path *geom.Path, source *models.CTColor, object models.PathObject, pb models.StBox, parentCTM *models.CTM, budget *renderBudget) bool {
	matrix := ctx.CurrentMatrix()
	if source == nil || source.Pattern == nil || !finiteMatrix(matrix) || geom.Equal(matrix.Det(), 0) || path == nil || path.Empty() {
		return false
	}
	opacity := graphicOpacity(compositeItemAlpha(source.Alpha, object.Alpha))
	if opacity == 0 {
		return true
	}
	bounds := path.Bounds()
	resolution := p.paintResolution(ctx)
	copies := 1.0
	if opacity != 255 {
		copies++
	}
	if !finiteFloat(bounds.X0) || !finiteFloat(bounds.Y0) || !finiteFloat(bounds.X1) || !finiteFloat(bounds.Y1) ||
		!budget.AllowOffscreenPixels(bounds.W(), bounds.H()*copies, resolution.DPI()) {
		return false
	}
	surface := newOffscreenSurface(bounds.W(), bounds.H(), resolution)
	if surface == nil {
		return false
	}
	surface.Translate(-bounds.X0, -bounds.Y0)
	if !p.drawPatternPath(surface, path, source.Pattern, object, pb, parentCTM, budget) {
		return false
	}
	img := surface.Raster()
	if img == nil || img.Bounds().Empty() {
		return false
	}
	box := imagelayout.CanvasBox(bounds, pb.Height)
	placement := imagelayout.BoundaryMatrix(box, float64(img.Bounds().Dx()), float64(img.Bounds().Dy()), nil, pb.Height)
	placement = matrix.Mul(placement)
	if !finiteMatrix(placement) {
		return false
	}
	ctx.RenderImage(applyImageAlpha(img, opacity), placement)
	return true
}

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
