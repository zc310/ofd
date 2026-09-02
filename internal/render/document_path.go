package render

import (
	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Path(ctx *canvas.Context, object models.PathObject, dp *models.DrawParam, pb models.StBox) {
	if !object.VisibleValue() {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	pa := p.buildObjectPath(object, pb.Height)
	fillColor := pathFillColor(object, dp)
	var pattern *models.CtPattern
	if fillColor != nil {
		pattern = fillColor.Pattern
	}

	p.updateCtPathStyle(ctx, &object.CtPath, dp)
	p.updatePathGradients(ctx, &object, dp, pb.Height)

	clipPath := p.buildPathClip(object.Clips, object.Boundary, pb.Height, object.CTM)
	if object.Fill && pattern != nil {
		fillPath := pa.Copy()
		fillPath.Close()
		if clipPath != nil {
			fillPath = fillPath.And(clipPath)
		}
		if p.drawPatternPath(ctx, fillPath, pattern, object.Boundary, pb, fillColor.Alpha) {
			object.Fill = false
		}
	}
	if clipPath == nil {
		ctx.DrawPath(0, 0, pa)
		return
	}
	p.drawClippedPath(ctx, pa, clipPath, object)
}

func pathFillColor(object models.PathObject, dp *models.DrawParam) *models.CTColor {
	if object.FillColor != nil && object.FillColor.Pattern != nil {
		return object.FillColor
	}
	if dp != nil && dp.FillColor != nil {
		return dp.FillColor
	}
	return nil
}

func (p *Document) buildObjectPath(object models.PathObject, pageHeight float64) *canvas.Path {
	box := object.Boundary
	transform := func(pt models.StPos) (float64, float64) {
		if object.CTM != nil {
			pt.X, pt.Y = object.CTM.TransformPoint(pt)
		}
		return pt.X + box.X, pageHeight - (pt.Y + box.Y)
	}
	return p.newPath(&object.CtPath, transform)
}

func (p *Document) buildPathClip(clips *models.Clips, box models.StBox, pageHeight float64, objectCTM *models.CTM) *canvas.Path {
	if clips == nil || len(clips.Clip) == 0 {
		return nil
	}

	objectMatrix := models.IdentityMatrix
	if objectCTM != nil {
		objectMatrix = *objectCTM
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
		if gradient := pathGradient(fillColor, transform); gradient != nil {
			ctx.SetFillGradient(gradient)
		}
	}
	if object.Stroke != "false" && strokeColor != nil {
		if gradient := pathGradient(strokeColor, transform); gradient != nil {
			ctx.SetStrokeGradient(gradient)
		}
	}
}

func pathGradient(ctColor *models.CTColor, transform func(models.StPos) canvas.Point) canvas.Gradient {
	if ctColor == nil {
		return nil
	}
	if shd := ctColor.AxialShd; shd != nil {
		return newOFDLinearGradient(shd, transform)
	}
	if shd := ctColor.RadialShd; shd != nil {
		return newOFDRadialGradient(shd, transform)
	}
	return nil
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
				cmd.Arc.SweepFlag,
				endX,
				endY,
			)

		case models.Close:
			pa.Close()
		}
	}
	return pa
}
