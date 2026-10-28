package render

import (
	"github.com/zc310/ofd/internal/models"
	compositelayout "github.com/zc310/ofd/internal/render/layout/composite"
	"github.com/zc310/ofd/internal/render/pathbuild"
)

// 复用绘制分支的尺寸兼容与墨迹采样。§13 表50的单元坐标先映射到 Boundary；
// 对需要裁掉透明边的兼容文档，文字框和链接也必须使用同一裁边范围。
// 实际绘制及预算留在 render，content 只接收测量后的区域与逻辑画布尺寸。
func (p *Document) compositeContentSource(object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, depth int, budget *renderBudget) (models.StBox, float64, float64, bool) {
	w, h := unit.Width, unit.Height
	if !object.Boundary.IsFinite() || !object.CTM.IsFinite() || w <= 0 || h <= 0 || !finiteFloat(w) || !finiteFloat(h) || object.Boundary.Width <= 0 || object.Boundary.Height <= 0 {
		return models.StBox{}, 0, 0, false
	}
	if ew, eh, ok := compositelayout.ContentExtent(object); ok && (compositelayout.ExtentMismatch(w, ew) || compositelayout.ExtentMismatch(h, eh)) {
		return models.StBox{Width: ew, Height: eh}, ew, eh, true
	}
	if len(unit.Content.Items) == 1 && unit.Content.Items[0].Path != nil && simpleCompositePath(*unit.Content.Items[0].Path) {
		b := pathbuild.Object(*unit.Content.Items[0].Path, h, nil).Bounds()
		return models.StBox{X: b.X0, Y: h - b.Y1, Width: b.W(), Height: b.H()}, w, h, !b.Empty() && b.W() > 0 && b.H() > 0
	}
	dpi := min(1200, max(10, p.dpi.DPI()*object.Boundary.Width/w))
	if !budget.AllowOffscreenPixels(w, h, dpi) {
		return models.StBox{}, 0, 0, false
	}
	raster := p.rasterizeCompositeUnit(unit, dp, models.StBox{Width: w, Height: h}, false, w, h, dpi, depth, budget)
	if compositeRasterBlank(raster) {
		// 隐形热区或 OCR 文字可以没有可见墨迹，此时保留完整单元坐标。
		return models.StBox{Width: w, Height: h}, w, h, true
	}
	if _, source, ok := p.resampleCompositeUnitSource(unit, dp, raster, w, h, dpi, object.Boundary, p.dpi.DPI(), depth, budget); ok {
		return source, w, h, true
	}
	x0, y0, x1, y1 := contentImageBounds(raster)
	step := 25.4 / dpi
	return models.StBox{X: x0 * step, Y: y0 * step, Width: (x1 - x0) * step, Height: (y1 - y0) * step}, w, h, true
}
