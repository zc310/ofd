package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// textOutlineContext 只收集文字的填充轮廓；嵌入 DrawContext 保留原排版变换，
// 不实现原生文字接口，使普通字体与 GID 字体都复用现有的轮廓绘制逻辑。
type textOutlineContext struct {
	DrawContext

	inverse geom.Matrix
	path    *geom.Path
}

func (c *textOutlineContext) DrawPath(x, y float64, path *geom.Path) {
	if path == nil || path.Empty() {
		return
	}
	matrix := c.inverse.Mul(c.CurrentMatrix()).Translate(x, y)
	outline := path.Copy().Transform(matrix)
	outline.Close()
	if c.path == nil {
		c.path = outline
	} else {
		c.path = c.path.Or(outline)
	}
}

func (p *Document) patternTextOutline(ctx DrawContext, object models.TextObject, pageHeight float64, parentCTM *models.CTM) *geom.Path {
	if p.fonts == nil {
		return nil
	}
	matrix := ctx.CurrentMatrix()
	if !finiteMatrix(matrix) || geom.Equal(matrix.Det(), 0) {
		return nil
	}
	family, err := p.fonts.LoadFont(object.Font)
	if err != nil || family == nil {
		return nil
	}
	lock := p.fonts.RenderLock(family)
	lock.Lock()
	defer lock.Unlock()
	object.Stroke = false
	object.Fill.Set(true)
	fill := &CTColor{Value: geom.Black, HasValue: true}
	face := p.fonts.FaceObject(family, object, fill)
	if face == nil {
		return nil
	}
	collector := &textOutlineContext{DrawContext: ctx, inverse: matrix.Inv()}
	faces := newTextFaces(p.fonts, family, object, fill, face, textHScale(object))
	object.CGTransform = models.ExpandCGTransformPositions(object.TextCode, object.CGTransform)
	position := 0
	for _, code := range object.TextCode {
		if finiteFloat(code.X) && finiteFloat(code.Y) && finiteArray(code.DeltaX) && finiteArray(code.DeltaY) {
			p.drawTextCode(collector, faces, object, code, pageHeight, parentCTM, position)
		}
		position += len([]rune(code.Value))
	}
	return collector.path
}

// drawPatternText 按 §8.3 表28把底纹裁到字形轮廓，再对整组应用颜色与图元透明度。
// 先完成轮廓收集并释放字体锁，底纹内引用同一字体时也能正常绘制。
func (p *Document) drawPatternText(ctx DrawContext, source *models.CTColor, object models.TextObject, pb models.StBox, parentCTM *models.CTM, clip *geom.Path, budget *renderBudget) {
	path := p.patternTextOutline(ctx, object, pb.Height, parentCTM)
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
		!budget.allowOffscreenPixels(bounds.W(), bounds.H(), resolution.DPI()) {
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
	placement := imageMatrix(box, img, models.CTM{box.Width, 0, 0, box.Height, 0, 0}, pb.Height)
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
