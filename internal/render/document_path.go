package render

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
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
	if !object.VisibleValue() || !object.CTM.IsFinite() || !parentCTM.IsFinite() ||
		!object.Boundary.IsFinite() || !pb.IsFinite() || !finiteFloat(pb.Height) {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	pa := p.buildObjectPathWithTransform(object, pb.Height, parentCTM)
	if pa.Empty() {
		return
	}

	p.updateCtPathStyle(ctx, &object.CtPath, dp)
	fillGradient, strokeGradient := p.updatePathGradients(ctx, &object, dp, pb.Height)

	clipPath := p.buildPathClip(object.Clips, object.Boundary, pb.Height, object.CTM, parentCTM)
	if parentClip != nil {
		if clipPath == nil {
			clipPath = parentClip
		} else {
			clipPath = clipPath.And(parentClip)
		}
	}
	var dpFillColor, dpStrokeColor *models.CTColor
	if dp != nil {
		dpFillColor = dp.FillColor
		dpStrokeColor = dp.StrokeColor
	}
	fillSource := resolvePathColor(object.FillColor, dpFillColor)
	strokeSource := resolvePathColor(object.StrokeColor, dpStrokeColor)
	fillIsMesh := isMeshColor(fillSource) || needsRasterGradient(fillGradient)
	var pattern *models.CtPattern
	if fillSource != nil {
		pattern = fillSource.Pattern
	}
	if object.Fill && fillIsMesh {
		fillPath := pa.Copy()
		fillPath.Close()
		if clipPath != nil {
			fillPath = fillPath.And(clipPath)
		}
		if p.drawMeshPaint(ctx, fillPath, fillGradient, object, pb, budget) {
			object.Fill = false
			ctx.ClearFill()
		}
	}
	if object.Stroke.Value(true) {
		if isMeshColor(strokeSource) || needsRasterGradient(strokeGradient) {
			strokePath := pa.Stroke(ctx.StrokeWidth(), ctx.StrokeCapper(), ctx.StrokeJoiner(), geom.Tolerance)
			if clipPath != nil {
				strokePath = strokePath.And(clipPath)
			}
			if p.drawMeshPaint(ctx, strokePath, strokeGradient, object, pb, budget) {
				object.Stroke.Set(false)
				ctx.ClearStroke()
			}
		}
	}
	if object.Fill && pattern != nil {
		fillPath := pa.Copy()
		fillPath.Close()
		if clipPath != nil {
			fillPath = fillPath.And(clipPath)
		}
		if p.drawPatternPath(ctx, fillPath, pattern, object, pb, parentCTM, budget) {
			object.Fill = false
			ctx.ClearFill()
		}
	}
	if clipPath == nil {
		ctx.DrawPath(0, 0, pa)
		return
	}
	p.drawClippedPath(ctx, pa, clipPath, object)
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

// needsRasterGradient 判断渐变是否无法写成 PDF 原生 Shading。
// Repeat/Reflect 只能采样，直接写入会得到没有 Coords/Function 的空着色。
// 起始半径为 0 的偏心径向渐变写成 ShadingType 3 后，焦点另一侧不会铺起点色。
func needsRasterGradient(gradient geom.Gradient) bool {
	switch value := gradient.(type) {
	case *ofdLinearGradient, *ofdRadialGradient, *ofdEllipticalGradient:
		return true
	case *geom.RadialGradient:
		return geom.Equal(value.R0, 0) && value.C0 != value.C1
	default:
		return false
	}
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
	resolution := geom.DPI(meshGradientDPI)
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
	if !budget.allowOffscreenPixels(width, height, meshGradientDPI) {
		return false
	}
	surface := newOffscreenSurface(width, height, resolution)
	surface.SetFillGradient(translateGradient(gradient, bounds.X0, bounds.Y0))
	surface.DrawPath(0, 0, paintPath.Copy().Translate(-bounds.X0, -bounds.Y0))
	var meshImage image.Image = surface.Raster()
	if meshImage == nil || meshImage.Bounds().Empty() {
		return false
	}

	if alpha != nil {
		meshImage = applyImageAlpha(meshImage, graphicOpacity(alpha))
	}
	// imageMatrix 的 Y 坐标是图片左上角在 PDF 页面坐标中的位置；
	// bounds 使用的是画布左下角坐标，因此这里需要先翻转到页面顶部坐标。
	box := models.StBox{
		X:      bounds.X0,
		Y:      pb.Height - bounds.Y1,
		Width:  width,
		Height: height,
	}
	matrix := imageMatrix(box, meshImage,
		models.CTM{width, 0, 0, height, 0, 0}, pb.Height)
	matrix = ctx.CurrentMatrix().Mul(matrix)
	if !finiteMatrix(matrix) {
		return false
	}
	ctx.RenderImage(meshImage, matrix)
	return true
}

// objectPointTransform 返回把对象局部坐标映射到画布坐标的变换：
// 先应用对象 CTM，再叠加对象 Boundary 偏移并翻转 Y 轴。
func objectPointTransform(object models.PathObject, pageHeight float64) func(models.StPos) geom.Point {
	return func(point models.StPos) geom.Point {
		if object.CTM != nil {
			point.X, point.Y = object.CTM.TransformPoint(point)
		}
		return geom.Point{X: point.X + object.Boundary.X, Y: pageHeight - (point.Y + object.Boundary.Y)}
	}
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

func (p *Document) buildObjectPath(object models.PathObject, pageHeight float64) *geom.Path {
	return p.buildObjectPathWithTransform(object, pageHeight, nil)
}

func (p *Document) buildObjectPathWithTransform(object models.PathObject, pageHeight float64, parentCTM *models.CTM) *geom.Path {
	if !object.CTM.IsFinite() || !parentCTM.IsFinite() || !object.Boundary.IsFinite() || !finiteFloat(pageHeight) {
		return &geom.Path{}
	}
	// Pattern 的 CellContent 边界属于父级图块的坐标系。
	// 父级与对象矩阵只组合一次，避免每个点重复执行矩阵乘法与分配。
	var ctm *models.CTM
	if parentCTM != nil {
		ctm = parentCTM
		if object.CTM != nil {
			ctm = parentCTM.Multiply(object.CTM)
			if !ctm.IsFinite() {
				return &geom.Path{}
			}
		}
	}
	box := object.Boundary
	objectTransform := objectPointTransform(object, pageHeight)
	transform := func(pt models.StPos) (float64, float64) {
		if ctm != nil {
			x, y := ctm.Transform(pt.X, pt.Y)
			return x + box.X, pageHeight - (y + box.Y)
		}
		point := objectTransform(pt)
		return point.X, point.Y
	}
	return p.newPath(&object.CtPath, transform)
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
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
			continue
		}
		if result == nil {
			result = region
		} else {
			result = result.And(region)
		}
	}
	return result
}

func (p *Document) buildClipRegion(clip models.CtClip, transFlag *bool, objectCTM models.CTM, box models.StBox, pageHeight float64) *geom.Path {
	var result *geom.Path
	for _, area := range clip.Area {
		if area.Path == nil {
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

		areaPath := p.newPath(area.Path, func(pt models.StPos) (float64, float64) {
			pt.X += area.Path.Boundary.X
			pt.Y += area.Path.Boundary.Y
			x, y := pathCTM.TransformPoint(pt)
			return x + box.X, pageHeight - (y + box.Y)
		})
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
	if object.Fill {
		// OFD 允许填充路径省略末尾闭合命令，布尔运算前需要补齐。
		fillPath := path.Copy()
		fillPath.Close()
		// Path.And 固定按 NonZero 规则求交，会把 Even-Odd 的洞当作填充区域。
		// 先用 Even-Odd 规则整理轮廓方向（外圈 CCW、洞 CW），求交后再按
		// NonZero 填充即可保留洞。
		if object.Rule == "Even-Odd" {
			fillPath = fillPath.Settle(geom.EvenOdd)
		}
		ctx.Push()
		ctx.SetStrokeColor(geom.Transparent)
		ctx.DrawPath(0, 0, fillPath.And(clip))
		ctx.Pop()
	}
	if object.Stroke.Value(true) {
		// 描边路径可能是开放路径，先转换为描边区域再执行裁剪。
		strokePath := path.Stroke(ctx.StrokeWidth(), ctx.StrokeCapper(), ctx.StrokeJoiner(), geom.Tolerance)
		ctx.Push()
		ctx.CopyStrokeToFill()
		ctx.SetStrokeColor(geom.Transparent)
		ctx.DrawPath(0, 0, strokePath.And(clip))
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
		if gradient := p.pathGradient(fillColor, transform); gradient != nil {
			fillGradient = gradient
			ctx.SetFillGradient(scaleGradientOpacity(gradient, graphicOpacity(object.Alpha)))
		}
	}
	if object.Stroke.Value(true) && strokeColor != nil {
		if gradient := p.pathGradient(strokeColor, transform); gradient != nil {
			strokeGradient = gradient
			ctx.SetStrokeGradient(scaleGradientOpacity(gradient, graphicOpacity(object.Alpha)))
		}
	}
	return fillGradient, strokeGradient
}

func (p *Document) pathGradient(ctColor *models.CTColor, transform func(models.StPos) geom.Point) geom.Gradient {
	if ctColor == nil {
		return nil
	}
	var gradient geom.Gradient
	if shd := ctColor.AxialShd; shd != nil {
		gradient = newOFDLinearGradient(shd, transform, p.colorRGBA)
	}
	if shd := ctColor.RadialShd; shd != nil {
		gradient = newOFDRadialGradient(shd, transform, p.colorRGBA)
	}
	if shd := ctColor.GouraudShd; shd != nil {
		gradient = newOFDGouraudGradient(shd, transform, p.colorRGBA)
	}
	if shd := ctColor.LaGourandShd; shd != nil {
		gradient = newOFDLaGouraudGradient(shd, transform, p.colorRGBA)
	}
	if shd := ctColor.LaGouraudShd; shd != nil {
		gradient = newOFDLaGouraudGradient(shd, transform, p.colorRGBA)
	}
	if gradient == nil {
		return nil
	}
	if ctColor.Alpha != nil {
		gradient = scaleGradientOpacity(gradient, *ctColor.Alpha)
	}
	return gradient
}

func (p *Document) newPath(cp *models.CtPath, transform func(pt models.StPos) (float64, float64)) *geom.Path {
	pa := &geom.Path{}
	if cp == nil || transform == nil {
		return pa
	}
	for _, cmd := range cp.AbbreviatedData {
		switch cmd.Type {
		case models.MoveTo, models.Start:
			if len(cmd.Points) < 1 {
				continue
			}
			x, y := transform(cmd.Points[0])
			if !finiteFloat(x) || !finiteFloat(y) {
				continue
			}
			pa.MoveTo(x, y)

		case models.LineTo:
			if len(cmd.Points) < 1 {
				continue
			}
			x, y := transform(cmd.Points[0])
			if !finiteFloat(x) || !finiteFloat(y) {
				continue
			}
			pa.LineTo(x, y)

		case models.QuadTo:
			if len(cmd.Points) < 2 {
				continue
			}
			cpx, cpy := transform(cmd.Points[0])
			x, y := transform(cmd.Points[1])
			if !finiteFloat(cpx) || !finiteFloat(cpy) || !finiteFloat(x) || !finiteFloat(y) {
				continue
			}
			pa.QuadTo(cpx, cpy, x, y)

		case models.CubicBezier:
			if len(cmd.Points) < 3 {
				continue
			}
			x1, y1 := transform(cmd.Points[0])
			x2, y2 := transform(cmd.Points[1])
			x3, y3 := transform(cmd.Points[2])
			if !finiteFloat(x1) || !finiteFloat(y1) || !finiteFloat(x2) || !finiteFloat(y2) || !finiteFloat(x3) || !finiteFloat(y3) {
				continue
			}
			pa.CubeTo(x1, y1, x2, y2, x3, y3)

		case models.ArcTo:
			if cmd.Arc == nil {
				continue
			}
			endX, endY := transform(cmd.Arc.EndPoint)
			if !finiteFloat(cmd.Arc.RX) || !finiteFloat(cmd.Arc.RY) || !finiteFloat(cmd.Arc.XAxisRotation) || !finiteFloat(endX) || !finiteFloat(endY) {
				continue
			}
			pa.ArcTo(
				cmd.Arc.RX,
				cmd.Arc.RY,
				cmd.Arc.XAxisRotation,
				cmd.Arc.LargeArcFlag,
				!cmd.Arc.SweepFlag,
				endX,
				endY,
			)

		case models.Close:
			pa.Close()
		}
	}
	return pa
}
