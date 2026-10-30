package render

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/internal/render/pathbuild"
	"github.com/zc310/ofd/pkg/spec"
)

const meshGradientDPI = 300.0

func (p *Document) Path(ctx DrawContext, object models.PathObject, dp *models.DrawParam, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.pathWithBudget(ctx, object, dp, pb, nil, nil, &budget)
}

// path 使用可选的父级变换绘制路径。Pattern 的 CellContent 对象与页面对象使用
// 相同的渲染器，并将图块变换作为父级变换传入。
func (p *Document) pathWithBudget(ctx DrawContext, object models.PathObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path, budget *renderBudget) {
	if !drawableGraphicUnit(object.VisibleValue(), object.CTM, parentCTM, object.Boundary, pb) {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	pa := pathbuild.Object(object, pb.Height, parentCTM)
	if pa.Empty() {
		return
	}

	strokeStyle := p.updateCtPathStyle(ctx, &object.CtPath, dp, paint.StrokeScale(object.CTM, parentCTM))
	fillGradient, strokeGradient := p.updatePathGradients(ctx, &object, dp, pb.Height)

	clipPath := p.pathGraphicClip(object, pb.Height, parentCTM, parentClip, ctx)
	bounds := pa.Bounds()
	if object.Stroke.Value(true) {
		bounds = strokeBounds(ctx, bounds)
	}
	if clipContainsRect(clipPath, bounds) {
		clipPath = nil
	}
	var dpFillColor, dpStrokeColor *models.CTColor
	if dp != nil {
		dpFillColor = dp.FillColor
		dpStrokeColor = dp.StrokeColor
	}
	fillSource := resolvePathColor(object.FillColor, dpFillColor)
	strokeSource := resolvePathColor(object.StrokeColor, dpStrokeColor)
	fillIsMesh := isMeshColor(fillSource) || paint.NeedsRaster(fillGradient, object.Alpha)
	var pattern *models.CtPattern
	if fillSource != nil {
		pattern = fillSource.Pattern
	}
	if object.Fill && fillIsMesh {
		fillPath := pa.Copy()
		fillPath.Close()
		if object.Rule == spec.FillRuleEvenOdd {
			fillPath = fillPath.Settle(geom.EvenOdd)
		}
		if clipPath != nil {
			fillPath = intersectClips(fillPath, clipPath)
		}
		if p.drawMeshPaint(ctx, fillPath, fillGradient, object, pb, budget) {
			object.Fill = false
			ctx.ClearFill()
		}
	}
	if object.Fill && pattern != nil {
		fillPath := pa.Copy()
		fillPath.Close()
		if object.Rule == spec.FillRuleEvenOdd {
			fillPath = fillPath.Settle(geom.EvenOdd)
		}
		if clipPath != nil {
			fillPath = intersectClips(fillPath, clipPath)
		}
		if p.drawPatternPath(ctx, fillPath, pattern, object, pb, parentCTM, budget) {
			object.Fill = false
			ctx.ClearFill()
		}
	}
	strokeRaster := object.Stroke.Value(true) && (isMeshColor(strokeSource) || paint.NeedsRaster(strokeGradient, object.Alpha))
	if strokeRaster && object.Fill {
		// 描边稍后会作为栅格图片绘制，且必须盖在填充之上。这里先把剩余的
		// 实色/矢量填充画掉；否则最后的 ctx.DrawPath 会用填充盖住描边内侧，
		// 左右描边只剩位于渐变轴外（Extend=0 时为透明）的外半条而整条消失。
		if clipPath == nil {
			ctx.Push()
			ctx.ClearStroke()
			ctx.DrawPath(0, 0, pa)
			ctx.Pop()
		} else {
			fillObject := object
			fillObject.Stroke.Set(false)
			p.drawClippedPath(ctx, pa, clipPath, fillObject)
		}
		object.Fill = false
		ctx.ClearFill()
	}
	if strokeRaster {
		strokePath := dashedStrokePath(ctx, pa, strokeStyle)
		if clipPath != nil {
			strokePath = intersectClips(strokePath, clipPath)
		}
		if p.drawMeshPaint(ctx, strokePath, strokeGradient, object, pb, budget) {
			object.Stroke.Set(false)
			ctx.ClearStroke()
		}
	}
	if clipPath == nil {
		ctx.DrawPath(0, 0, pa)
		return
	}
	p.drawClippedPathStyle(ctx, pa, clipPath, object, strokeStyle)
}

// resolvePathColor 返回路径实际使用的颜色：对象颜色优先，缺省时回退到 DrawParam。
func resolvePathColor(color, fallback *models.CTColor) *models.CTColor {
	if color != nil {
		return color
	}
	return fallback
}

func isMeshColor(color *models.CTColor) bool {
	return color != nil && (color.GouraudShd != nil || color.LaGourandShd != nil || color.LaGouraudShd != nil)
}

// isShadingColor 判断颜色是否使用着色渐变（网格、轴向或径向）。这些渐变作用在
// 文字上时无法写成合法的 PDF 原生 Shading：文字渐变必须映射到每个 run 的排版
// 空间，会变成自定义 Gradient，PDF 后端只能写出没有 Coords/Function 的空
// /Shading。因此文字着色渐变统一先栅格化再以图片绘制。
func isShadingColor(color *models.CTColor) bool {
	return isMeshColor(color) || (color != nil && (color.AxialShd != nil || color.RadialShd != nil))
}

// drawMeshPaint 将网格渐变先栅格化，再以图像方式绘制到目标画布。
// PDF/SVG 等矢量输出不支持自定义渐变，使用图像回退可以保证
// Gouraud/LaGourand 在不同输出格式下都能保留视觉效果。
// gradient 由调用方预先构造（不含对象 Alpha，对象 Alpha 在栅格图上应用）。
func (p *Document) drawMeshPaint(ctx DrawContext, paintPath *geom.Path, gradient geom.Gradient, object models.PathObject, pb models.StBox, budget *renderBudget) bool {
	if paintPath == nil || gradient == nil {
		return false
	}
	return p.drawMeshPaintGradient(ctx, paintPath, gradient, pb, object.Alpha, budget)
}

func (p *Document) drawMeshPaintGradient(ctx DrawContext, paintPath *geom.Path, gradient geom.Gradient, pb models.StBox, alpha *uint8, budget *renderBudget) bool {
	if paintPath == nil || gradient == nil || !pb.IsFinite() || pb.Width <= 0 || pb.Height <= 0 {
		return false
	}

	// 只为实际路径创建离屏画布。网格渐变最终会作为图片嵌入 PDF，
	// 创建整页 A4 位图会让每个网格对象都承担整页的栅格化和压缩成本。
	bounds := paintPath.Bounds()
	if bounds.Empty() || bounds.W() <= 0 || bounds.H() <= 0 {
		return false
	}
	resolution := p.paintResolution(ctx)
	padding := 1.0 / resolution.DPMM()
	bounds.X0 -= padding
	bounds.Y0 -= padding
	bounds.X1 += padding
	bounds.Y1 += padding
	width, height := bounds.W(), bounds.H()
	if !finiteFloat(bounds.X0) || !finiteFloat(bounds.Y0) || !finiteFloat(bounds.X1) || !finiteFloat(bounds.Y1) ||
		!finiteFloat(width) || !finiteFloat(height) || width <= 0 || height <= 0 {
		return false
	}
	if !budget.AllowOffscreenPixels(width, height, resolution.DPI()) {
		return false
	}
	surface := newOffscreenSurface(width, height, resolution)
	if surface == nil {
		return false
	}
	surface.SetFillGradient(paint.TranslateGradient(gradient, bounds.X0, bounds.Y0))
	surface.DrawPath(0, 0, paintPath.Copy().Translate(-bounds.X0, -bounds.Y0))
	var meshImage image.Image = surface.Raster()
	if meshImage == nil || meshImage.Bounds().Empty() {
		return false
	}

	if alpha != nil {
		meshImage = applyImageAlpha(meshImage, graphicOpacity(alpha))
	}
	// imagelayout.Matrix 的 Y 坐标是图片左上角在 PDF 页面坐标中的位置；
	// bounds 使用的是画布左下角坐标，因此这里需要先翻转到页面顶部坐标。
	box := models.StBox{
		X:      bounds.X0,
		Y:      pb.Height - bounds.Y1,
		Width:  width,
		Height: height,
	}
	matrix := imagelayout.Matrix(box, meshImage,
		models.CTM{width, 0, 0, height, 0, 0}, pb.Height)
	matrix = ctx.CurrentMatrix().Mul(matrix)
	if !finiteMatrix(matrix) {
		return false
	}
	ctx.RenderImage(meshImage, matrix)
	return true
}

// gradientBoundaryTransform 返回把渐变坐标映射到画布坐标的变换。
// OFD 的渐变坐标位于对象 CTM 已经生效的坐标系中（与路径数据的局部坐标系不同），
// 因此这里不再应用对象 CTM，只叠加 Boundary 偏移并翻转 Y 轴；否则带平移或缩放的
// CTM 会被重复应用，导致渐变整体偏离图形，只能看到端点颜色。
func gradientBoundaryTransform(boundary models.StBox, pageHeight float64) func(models.StPos) geom.Point {
	return func(point models.StPos) geom.Point {
		return geom.Point{X: point.X + boundary.X, Y: pageHeight - (point.Y + boundary.Y)}
	}
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// drawableGraphicUnit 校验图元绘制所需的公共入参：可见性、对象与父级 CTM、
// Boundary 和页面区域都必须是有限数值。
//
// OFD 允许这些属性缺失或被写入非有限值，非法输入一律跳过绘制而不是让 NaN
// 坐标进入变换矩阵（那会污染整个内容流）。文字、路径、图片和复合图元四条
// 绘制入口共用该规则，单独实现容易在新增校验项时漏改某一处。
func drawableGraphicUnit(visible bool, ctm, parentCTM *models.CTM, boundary models.StBox, pb models.StBox) bool {
	return visible && ctm.IsFinite() && parentCTM.IsFinite() &&
		boundary.IsFinite() && pb.IsFinite() && finiteFloat(pb.Height)
}

func (p *Document) buildPathClip(clips *models.Clips, box models.StBox, pageHeight float64, objectCTM, parentCTM *models.CTM) *geom.Path {
	if clips == nil || len(clips.Clip) == 0 {
		return nil
	}

	objectMatrix := models.IdentityMatrix
	if objectCTM != nil {
		if !objectCTM.IsFinite() {
			return nil
		}
		objectMatrix = *objectCTM
	}
	if parentCTM != nil {
		if !parentCTM.IsFinite() {
			return nil
		}
		objectMatrix = *parentCTM.Multiply(&objectMatrix)
		if !objectMatrix.IsFinite() {
			return nil
		}
	}

	var result *geom.Path
	for _, clip := range clips.Clip {
		region := p.buildClipRegion(clip, clips.TransFlag, objectMatrix, box, pageHeight)
		if region == nil {
			region = &geom.Path{}
		}
		if result == nil {
			result = region
		} else {
			result = intersectClips(result, region)
		}
	}
	return result
}

func (p *Document) buildClipRegion(clip models.CtClip, transFlag *bool, objectCTM models.CTM, box models.StBox, pageHeight float64) *geom.Path {
	var result *geom.Path
	for _, area := range clip.Area {
		if area.Path == nil && area.Text == nil {
			continue
		}

		areaCTM := models.IdentityMatrix
		if area.CTM != nil {
			if !area.CTM.IsFinite() {
				continue
			}
			areaCTM = *area.CTM
		}
		if transFlag == nil || *transFlag {
			areaCTM = *objectCTM.Multiply(&areaCTM)
		}
		var areaPath *geom.Path
		if area.Text != nil {
			// 文字裁剪区：用字形轮廓作为区域。Clips/Clip/Area 允许 Path 与 Text
			// 二选一，此前只处理 Path，文字区域被静默跳过；区域全部落空时
			// buildPathClip 返回 nil，裁剪整体失效、图元被完整画出。
			areaPath = p.buildTextClipPath(area.Text, areaCTM, box, pageHeight)
		} else {
			pathCTM := areaCTM
			if area.Path.CTM != nil {
				if !area.Path.CTM.IsFinite() {
					continue
				}
				pathCTM = *areaCTM.Multiply(area.Path.CTM)
			}
			if !pathCTM.IsFinite() {
				continue
			}

			areaPath = pathbuild.New(area.Path, func(pt models.StPos) (float64, float64) {
				pt.X += area.Path.Boundary.X
				pt.Y += area.Path.Boundary.Y
				x, y := pathCTM.TransformPoint(pt)
				return x + box.X, pageHeight - (y + box.Y)
			})
		}
		if areaPath == nil || areaPath.Empty() {
			continue
		}
		// Clip 区域用于填充，参与布尔运算前必须闭合。
		areaPath.Close()
		if result == nil {
			result = areaPath
		} else {
			result = result.Or(areaPath)
		}
	}
	return result
}

func (p *Document) drawClippedPath(ctx DrawContext, path, clip *geom.Path, object models.PathObject) {
	p.drawClippedPathStyle(ctx, path, clip, object, object.CTGraphicUnit)
}

func dashedStrokePath(ctx DrawContext, path *geom.Path, style models.CTGraphicUnit) *geom.Path {
	if paint.ValidDashPattern(style.DashOffset, style.DashPattern) {
		path = path.Dash(style.DashOffset, (*style.DashPattern)...)
	}
	return path.Stroke(strokeOutlineWidth(ctx), ctx.StrokeCapper(), ctx.StrokeJoiner(), geom.Tolerance)
}

func (p *Document) drawClippedPathStyle(ctx DrawContext, path, clip *geom.Path, object models.PathObject, style models.CTGraphicUnit) {
	if object.Fill {
		fillPath, rule := path, geom.NonZero
		if object.Rule == spec.FillRuleEvenOdd {
			rule = geom.EvenOdd
		}
		// 填充与描边分别判断：填充已完整覆盖时保留原路径及填充规则，
		// 无需因描边超出裁剪区而一同展开、整理和求交。
		if !clipContainsRect(clip, path.Bounds()) {
			fillPath = path.Copy()
			fillPath.Close()
			// Path.And 固定按 NonZero 求交，Even-Odd 先整理轮廓方向以保留洞。
			if rule == geom.EvenOdd {
				fillPath = fillPath.Settle(rule)
			}
			fillPath, rule = fillPath.And(clip), geom.NonZero
		}
		ctx.Push()
		ctx.SetFillRule(rule)
		ctx.SetStrokeColor(geom.Transparent)
		ctx.DrawPath(0, 0, fillPath)
		ctx.Pop()
	}
	if object.Stroke.Value(true) {
		// 描边路径可能是开放路径，先转换为描边区域再执行裁剪。
		strokePath := dashedStrokePath(ctx, path, style)
		ctx.Push()
		ctx.SetFillRule(geom.NonZero)
		ctx.CopyStrokeToFill()
		ctx.SetStrokeColor(geom.Transparent)
		ctx.DrawPath(0, 0, intersectClips(strokePath, clip))
		ctx.Pop()
	}
}

// updatePathGradients 设置绘制的填充/描边渐变，并返回未经对象 Alpha 缩放的
// 原生渐变；网格渐变回退绘制时复用该渐变，只在栅格图像上应用对象 Alpha。
func (p *Document) updatePathGradients(ctx DrawContext, object *models.PathObject, dp *models.DrawParam, pageHeight float64) (fillGradient, strokeGradient geom.Gradient) {
	fillColor := object.FillColor
	strokeColor := object.StrokeColor
	if dp != nil {
		fillColor = resolvePathColor(fillColor, dp.FillColor)
		strokeColor = resolvePathColor(strokeColor, dp.StrokeColor)
	}

	transform := gradientBoundaryTransform(object.Boundary, pageHeight)
	if object.Fill && fillColor != nil {
		if gradient := p.pathGradient(fillColor, transform, gradientShadingArea(object.Boundary)); gradient != nil {
			fillGradient = gradient
			ctx.SetFillGradient(paint.ScaleOpacity(gradient, graphicOpacity(object.Alpha)))
		}
	}
	if object.Stroke.Value(true) && strokeColor != nil {
		if gradient := p.pathGradient(strokeColor, transform, gradientShadingArea(object.Boundary)); gradient != nil {
			strokeGradient = gradient
			ctx.SetStrokeGradient(paint.ScaleOpacity(gradient, graphicOpacity(object.Alpha)))
		}
	}
	return fillGradient, strokeGradient
}

// gradientShadingArea 返回对象 Boundary 在渐变局部坐标下的范围。
// gradientBoundaryTransform 把局部 (0,0)-(Width,Height) 映射到 Boundary，
// 因此着色区域就是局部坐标下的这个矩形。尺寸非法时返回 nil。
func gradientShadingArea(boundary models.StBox) *geom.Rect {
	if !boundary.IsFinite() || boundary.Width <= 0 || boundary.Height <= 0 {
		return nil
	}
	area := geom.RectFromSize(0, 0, boundary.Width, boundary.Height)
	return &area
}
