package render

import (
	"image/color"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

func (p *Document) updateDrawParams(ctx *canvas.Context, dp *models.DrawParam) (*CTColor, *CTColor) {
	if dp == nil {
		return nil, nil
	}

	if dp.StrokeColor != nil && dp.StrokeColor.Value != nil {
		ctx.SetStrokeColor(dp.StrokeColor.Value.RGBA)
	}
	ctx.SetStrokeWidth(max(dp.LineWidth, 1))
	if dp.DashPattern != nil {
		ctx.SetDashes(dp.DashOffset, *dp.DashPattern...)
	}

	ctx.SetStrokeCapper(getLineCap(dp.Cap))
	ctx.SetStrokeJoiner(getLineJoin(dp.Join))

	return p.updateCtColor(dp.FillColor), p.updateCtColor(dp.StrokeColor)
}

type CTColor struct {
	Value    *color.RGBA
	AxialShd canvas.Gradient
}

func (p *Document) updateCtColor(object *models.CTColor) *CTColor {
	if object == nil {
		return nil
	}
	cc := &CTColor{}
	if object.Value != nil {
		cc.Value = &object.Value.RGBA
		// 颜色透明度，在 0~255 之间取值。默认为 255，表示完可选全不透明
		if object.Alpha != nil && *object.Alpha < 255 {
			cc.Value.A = 255 - *object.Alpha
		}
	}

	// 底纹填充
	pattern := object.Pattern
	if pattern != nil {

	}

	//轴向渐变
	axialShd := object.AxialShd
	if axialShd != nil {
		startPoint := axialShd.StartPoint
		endPoint := axialShd.EndPoint
		start := canvas.Point{X: startPoint.X, Y: startPoint.Y}
		end := canvas.Point{X: endPoint.X, Y: endPoint.Y}
		gradient := canvas.NewLinearGradient(start, end)
		if len(axialShd.Segment) == 2 && axialShd.Segment[0].Position == 0 && axialShd.Segment[1].Position == 0 {
			axialShd.Segment[1].Position = 1
		}
		for _, segment := range axialShd.Segment {
			gradient.Add(segment.Position, segment.Color.Value.RGBA)
		}

		cc.AxialShd = gradient
	}
	///径向渐变
	shd := object.RadialShd
	if shd != nil {
		startPoint := shd.StartPoint
		endPoint := shd.EndPoint
		start := canvas.Point{X: startPoint.X, Y: startPoint.Y}
		end := canvas.Point{X: endPoint.X, Y: endPoint.Y}
		gradient := canvas.NewRadialGradient(start, shd.StartRadius, end, shd.EndRadius)

		for _, segment := range shd.Segment {
			gradient.Add(segment.Position, segment.Color.Value.RGBA)
		}
		cc.AxialShd = gradient

	}
	return cc
}

func getLineCap(capStr string) canvas.Capper {
	switch capStr {
	case "Round":
		return canvas.RoundCap
	case "Square":
		return canvas.SquareCap
	default:
		return canvas.ButtCap
	}
}
func getLineJoin(capStr string) canvas.Joiner {
	switch capStr {
	case "Round":
		return canvas.RoundJoin
	case "Bevel":
		return canvas.BevelJoin
	default:
		return canvas.MiterJoin
	}
}
func (p *Document) updateCtPathStyle(ctx *canvas.Context, object *models.CtPath, dp *models.DrawParam) {
	if object == nil {
		return
	}
	strokeColor := canvas.Black
	fillColor := canvas.Black
	fill, stroke := p.updateDrawParams(ctx, dp)

	if object.FillColor != nil {
		fill = p.updateCtColor(object.FillColor)
	}
	if object.Fill {
		if fill != nil {
			if fill.Value != nil {
				fillColor = *fill.Value
				if object.Alpha != nil {
					fillColor.A = 255 - *object.Alpha
				}
				ctx.SetFillColor(fillColor)
			}

			if fill.AxialShd != nil {
				ctx.SetFillGradient(fill.AxialShd)
			}
		}
		if object.Rule == "Even-Odd" {
			ctx.FillRule = canvas.EvenOdd
		}
	} else {
		ctx.SetFill(nil)
	}

	if object.StrokeColor != nil {
		stroke = p.updateCtColor(object.StrokeColor)
	}
	if object.Stroke != "false" {
		ctx.SetStrokeWidth(max(object.LineWidth, 1) * 0.353)
		if stroke != nil {
			if stroke.Value != nil {
				strokeColor = *stroke.Value
				ctx.SetStrokeColor(strokeColor)
			}
			if stroke.AxialShd != nil {
				ctx.SetStrokeGradient(stroke.AxialShd)
			}
		} else {
			ctx.SetStrokeColor(strokeColor)
		}
		ctx.SetStrokeCapper(getLineCap(object.Cap))
		joiner := getLineJoin(object.Join)
		if joiner == canvas.MiterJoin {
			if object.MiterLimit == 0 {
				object.MiterLimit = 3.528
			}
			joiner = canvas.MiterJoiner{GapJoiner: canvas.BevelJoin, Limit: object.MiterLimit}
		}
		ctx.SetStrokeJoiner(joiner)
	} else {
		ctx.SetStrokeWidth(-1)
	}

	if object.DashPattern != nil {
		ctx.SetDashes(object.DashOffset, *object.DashPattern...)
	}
}
