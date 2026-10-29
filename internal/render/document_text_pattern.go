package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
)

// drawPatternText 按 §8.3 表28把底纹裁到字形轮廓，再对整组应用颜色与图元透明度。
// 先完成轮廓收集并释放字体锁，底纹内引用同一字体时也能正常绘制。
func (p *Document) drawPatternText(ctx DrawContext, source *models.CTColor, object models.TextObject, pb models.StBox, parentCTM *models.CTM, clip *geom.Path, budget *renderBudget) {
	matrix := ctx.CurrentMatrix()
	if !finiteMatrix(matrix) || geom.Equal(matrix.Det(), 0) {
		return
	}
	path := p.patternTextOutline(object, pb.Height, parentCTM)
	if path == nil || path.Empty() {
		return
	}
	if clip != nil {
		path = path.And(clip)
	}
	if path.Empty() {
		return
	}
	bounds := path.Bounds()
	resolution := p.paintResolution(ctx)
	if !finiteFloat(bounds.X0) || !finiteFloat(bounds.Y0) || !finiteFloat(bounds.X1) || !finiteFloat(bounds.Y1) ||
		!budget.AllowOffscreenPixels(bounds.W(), bounds.H(), resolution.DPI()) {
		return
	}
	surface := newOffscreenSurface(bounds.W(), bounds.H(), resolution)
	if surface == nil {
		return
	}
	surface.Translate(-bounds.X0, -bounds.Y0)
	pathObject := models.PathObject{CtPath: models.CtPath{CTGraphicUnit: object.CTGraphicUnit, Fill: true}}
	if !p.drawPatternPath(surface, path, source.Pattern, pathObject, pb, parentCTM, budget) {
		return
	}
	img := surface.Raster()
	if img == nil || img.Bounds().Empty() {
		return
	}
	box := models.StBox{X: bounds.X0, Y: pb.Height - bounds.Y1, Width: bounds.W(), Height: bounds.H()}
	placement := imagelayout.Matrix(box, img, models.CTM{box.Width, 0, 0, box.Height, 0, 0}, pb.Height)
	opacity := graphicOpacity(compositeItemAlpha(source.Alpha, object.Alpha))
	if opacity == 0 {
		return
	}
	prepared := imageWithClipAlpha(img, nil, placement, opacity)
	placement = ctx.CurrentMatrix().Mul(placement)
	if !finiteMatrix(placement) {
		return
	}
	ctx.RenderImage(prepared, placement)
	// 位图保存底纹的可见效果，原排版另输出不可见文字以供 PDF 复制与搜索。
	object.FillColor = &models.CTColor{Value: &models.Color{RGBA: geom.Black}}
	object.Stroke, object.Alpha = false, nil
	object.Fill.Set(true)
	p.textWithBudget(semanticOnlyContext{ctx}, object, nil, pb, parentCTM, clip, budget)
}
