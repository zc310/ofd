package render

import (
	"image"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
)

const meshGradientDPI = 300.0

func (p *Document) Path(ctx *canvas.Context, object models.PathObject, dp *models.DrawParam, pb models.StBox) {
	p.path(ctx, object, dp, pb, nil, nil)
}

// path 使用可选的父级变换绘制路径。Pattern 的 CellContent 对象与页面对象使用
// 相同的渲染器，并将图块变换作为父级变换传入。
func (p *Document) path(ctx *canvas.Context, object models.PathObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path) {
	if !object.VisibleValue() {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	pa := p.buildObjectPathWithTransform(object, pb.Height, parentCTM)

	p.updateCtPathStyle(ctx, &object.CtPath, dp)
	p.updatePathGradients(ctx, &object, dp, pb.Height)

	clipPath := p.buildPathClip(object.Clips, object.Boundary, pb.Height, object.CTM, parentCTM)
	if parentClip != nil {
		if clipPath == nil {
			clipPath = parentClip
		} else {
			clipPath = clipPath.And(parentClip)
		}
	}
	fillSource := object.FillColor
	var pattern *models.CtPattern
	if fillSource == nil && dp != nil {
		fillSource = dp.FillColor
	}
	if fillSource != nil {
		pattern = fillSource.Pattern
	}
	if object.Fill && isMeshColor(fillSource) {
		fillPath := pa.Copy()
		fillPath.Close()
		if clipPath != nil {
			fillPath = fillPath.And(clipPath)
		}
		if p.drawMeshPaint(ctx, fillPath, fillSource, object, pb) {
			object.Fill = false
			ctx.SetFill(nil)
		}
	}
	if object.Stroke != "false" {
		strokeSource := object.StrokeColor
		if strokeSource == nil && dp != nil {
			strokeSource = dp.StrokeColor
		}
		if isMeshColor(strokeSource) {
			strokePath := pa.Stroke(ctx.Style.StrokeWidth, ctx.Style.StrokeCapper, ctx.Style.StrokeJoiner, canvas.Tolerance)
			if clipPath != nil {
				strokePath = strokePath.And(clipPath)
			}
			if p.drawMeshPaint(ctx, strokePath, strokeSource, object, pb) {
				object.Stroke = "false"
				ctx.SetStroke(nil)
			}
		}
	}
	if object.Fill && pattern != nil {
		fillPath := pa.Copy()
		fillPath.Close()
		if clipPath != nil {
			fillPath = fillPath.And(clipPath)
		}
		if p.drawPatternPath(ctx, fillPath, pattern, object, pb, parentCTM) {
			object.Fill = false
			ctx.SetFill(nil)
		}
	}
	if clipPath == nil {
		ctx.DrawPath(0, 0, pa)
		return
	}
	p.drawClippedPath(ctx, pa, clipPath, object)
}

func isMeshColor(color *models.CTColor) bool {
	return color != nil && (color.GouraudShd != nil || color.LaGourandShd != nil || color.LaGouraudShd != nil)
}

// drawMeshPaint 将网格渐变先栅格化，再以图像方式绘制到目标画布。
// canvas 的 PDF 和 SVG 渲染器不支持自定义渐变，使用图像回退可以保证
// Gouraud/LaGourand 在不同输出格式下都能保留视觉效果。
func (p *Document) drawMeshPaint(ctx *canvas.Context, paintPath *canvas.Path, source *models.CTColor, object models.PathObject, pb models.StBox) bool {
	if paintPath == nil || source == nil {
		return false
	}
	transform := func(point models.StPos) canvas.Point {
		if object.CTM != nil {
			point.X, point.Y = object.CTM.TransformPoint(point)
		}
		return canvas.Point{X: point.X + object.Boundary.X, Y: pb.Height - (point.Y + object.Boundary.Y)}
	}
	gradient := p.pathGradient(source, transform)
	if gradient == nil {
		return false
	}
	return p.drawMeshPaintGradient(ctx, paintPath, gradient, pb, object.Alpha)
}

func (p *Document) drawMeshPaintGradient(ctx *canvas.Context, paintPath *canvas.Path, gradient canvas.Gradient, pb models.StBox, alpha *uint8) bool {
	if paintPath == nil || gradient == nil || pb.Width <= 0 || pb.Height <= 0 {
		return false
	}

	// 使用与页面等大的离屏画布，让网格渐变直接以页面坐标采样，
	// 与 drawMeshText 保持一致，避免局部画布带来的坐标翻转与渐变平移问题。
	page := canvas.New(pb.Width, pb.Height)
	pageCtx := canvas.NewContext(page)
	pageCtx.SetFillGradient(gradient)
	pageCtx.DrawPath(0, 0, paintPath)
	var meshImage image.Image = rasterizer.Draw(page, canvas.DPI(meshGradientDPI), canvas.DefaultColorSpace)
	if meshImage == nil || meshImage.Bounds().Empty() {
		return false
	}

	if alpha != nil {
		meshImage = applyImageAlpha(meshImage, graphicOpacity(alpha))
	}
	matrix := imageMatrix(models.StBox{Width: pb.Width, Height: pb.Height}, meshImage,
		models.CTM{pb.Width, 0, 0, pb.Height, 0, 0}, pb.Height)
	ctx.RenderImage(meshImage, ctx.CoordSystemView().Mul(ctx.View()).Mul(matrix))
	return true
}

func (p *Document) buildObjectPath(object models.PathObject, pageHeight float64) *canvas.Path {
	return p.buildObjectPathWithTransform(object, pageHeight, nil)
}

func (p *Document) buildObjectPathWithTransform(object models.PathObject, pageHeight float64, parentCTM *models.CTM) *canvas.Path {
	box := object.Boundary
	transform := func(pt models.StPos) (float64, float64) {
		if parentCTM != nil {
			ctm := parentCTM
			if object.CTM != nil {
				ctm = parentCTM.Multiply(object.CTM)
			}
			// Pattern 的 CellContent 边界属于父级图块的坐标系，
			// 与 ofdgo 中 boundaryInCTM=false 的行为一致。
			x, y := ctm.Transform(pt.X, pt.Y)
			return x + box.X, pageHeight - (y + box.Y)
		}
		if object.CTM != nil {
			pt.X, pt.Y = object.CTM.TransformPoint(pt)
		}
		return pt.X + box.X, pageHeight - (pt.Y + box.Y)
	}
	return p.newPath(&object.CtPath, transform)
}

func (p *Document) buildPathClip(clips *models.Clips, box models.StBox, pageHeight float64, objectCTM, parentCTM *models.CTM) *canvas.Path {
	if clips == nil || len(clips.Clip) == 0 {
		return nil
	}

	objectMatrix := models.IdentityMatrix
	if objectCTM != nil {
		objectMatrix = *objectCTM
	}
	if parentCTM != nil {
		objectMatrix = *parentCTM.Multiply(&objectMatrix)
	}

	var result *canvas.Path
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

func (p *Document) buildClipRegion(clip models.CtClip, transFlag *bool, objectCTM models.CTM, box models.StBox, pageHeight float64) *canvas.Path {
	var result *canvas.Path
	for _, area := range clip.Area {
		if area.Path == nil {
			continue
		}

		areaCTM := models.IdentityMatrix
		if area.CTM != nil {
			areaCTM = *area.CTM
		}
		if transFlag == nil || *transFlag {
			areaCTM = *objectCTM.Multiply(&areaCTM)
		}
		pathCTM := areaCTM
		if area.Path.CTM != nil {
			pathCTM = *areaCTM.Multiply(area.Path.CTM)
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

func (p *Document) drawClippedPath(ctx *canvas.Context, path, clip *canvas.Path, object models.PathObject) {
	if object.Fill {
		// OFD 允许填充路径省略末尾闭合命令，布尔运算前需要补齐。
		fillPath := path.Copy()
		fillPath.Close()
		ctx.Push()
		ctx.SetStrokeColor(canvas.Transparent)
		ctx.DrawPath(0, 0, fillPath.And(clip))
		ctx.Pop()
	}
	if object.Stroke != "false" {
		// 描边路径可能是开放路径，先转换为描边区域再执行裁剪。
		strokePath := path.Stroke(ctx.Style.StrokeWidth, ctx.Style.StrokeCapper, ctx.Style.StrokeJoiner, canvas.Tolerance)
		ctx.Push()
		ctx.SetFill(ctx.Style.Stroke)
		ctx.SetStrokeColor(canvas.Transparent)
		ctx.DrawPath(0, 0, strokePath.And(clip))
		ctx.Pop()
	}
}

func (p *Document) updatePathGradients(ctx *canvas.Context, object *models.PathObject, dp *models.DrawParam, pageHeight float64) {
	fillColor := object.FillColor
	strokeColor := object.StrokeColor
	if fillColor == nil && dp != nil {
		fillColor = dp.FillColor
	}
	if strokeColor == nil && dp != nil {
		strokeColor = dp.StrokeColor
	}

	transform := func(point models.StPos) canvas.Point {
		if object.CTM != nil {
			point.X, point.Y = object.CTM.TransformPoint(point)
		}
		return canvas.Point{X: point.X + object.Boundary.X, Y: pageHeight - (point.Y + object.Boundary.Y)}
	}
	if object.Fill && fillColor != nil {
		if gradient := p.pathGradient(fillColor, transform); gradient != nil {
			ctx.SetFillGradient(gradient)
		}
	}
	if object.Stroke != "false" && strokeColor != nil {
		if gradient := p.pathGradient(strokeColor, transform); gradient != nil {
			ctx.SetStrokeGradient(gradient)
		}
	}
}

func (p *Document) pathGradient(ctColor *models.CTColor, transform func(models.StPos) canvas.Point) canvas.Gradient {
	if ctColor == nil {
		return nil
	}
	var gradient canvas.Gradient
	if shd := ctColor.AxialShd; shd != nil {
		gradient = newOFDLinearGradient(shd, transform)
	}
	if shd := ctColor.RadialShd; shd != nil {
		gradient = newOFDRadialGradient(shd, transform)
	}
	if shd := ctColor.GouraudShd; shd != nil {
		gradient = newOFDGouraudGradient(shd, transform)
	}
	if shd := ctColor.LaGourandShd; shd != nil {
		gradient = newOFDLaGouraudGradient(shd, transform)
	}
	if shd := ctColor.LaGouraudShd; shd != nil {
		gradient = newOFDLaGouraudGradient(shd, transform)
	}
	if gradient == nil {
		return nil
	}
	if ctColor.Alpha != nil {
		gradient = scaleGradientOpacity(gradient, *ctColor.Alpha)
	}
	return gradient
}

func (p *Document) newPath(cp *models.CtPath, transform func(pt models.StPos) (float64, float64)) *canvas.Path {
	pa := &canvas.Path{}
	for _, cmd := range cp.AbbreviatedData {
		switch cmd.Type {
		case models.MoveTo, models.Start:
			x, y := transform(cmd.Points[0])
			pa.MoveTo(x, y)

		case models.LineTo:
			x, y := transform(cmd.Points[0])
			pa.LineTo(x, y)

		case models.QuadTo:
			cpx, cpy := transform(cmd.Points[0])
			x, y := transform(cmd.Points[1])
			pa.QuadTo(cpx, cpy, x, y)

		case models.CubicBezier:
			x1, y1 := transform(cmd.Points[0])
			x2, y2 := transform(cmd.Points[1])
			x3, y3 := transform(cmd.Points[2])
			pa.CubeTo(x1, y1, x2, y2, x3, y3)

		case models.ArcTo:
			endX, endY := transform(cmd.Arc.EndPoint)
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
