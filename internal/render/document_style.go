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

	if dp.StrokeColor != nil {
		p.setColor(ctx.SetStrokeColor, dp.StrokeColor)
	}
	lineWidth := dp.LineWidth
	if lineWidth == 0 {
		lineWidth = 0.353
	}
	ctx.SetStrokeWidth(lineWidth)
	if dp.DashPattern != nil {
		ctx.SetDashes(dp.DashOffset, *dp.DashPattern...)
	}

	ctx.SetStrokeCapper(getLineCap(dp.Cap))
	ctx.SetStrokeJoiner(getLineJoin(dp.Join))

	return p.updateCtColor(dp.FillColor), p.updateCtColor(dp.StrokeColor)
}

type CTColor struct {
	Value    *color.RGBA
	Gradient canvas.Gradient
}

func (p *Document) updateCtColor(source *models.CTColor) *CTColor {
	if source == nil {
		return nil
	}
	cc := &CTColor{}
	if source.Value != nil {
		value := ofdColorRGBA(*source)
		cc.Value = &value
	}

	cc.Gradient = p.pathGradient(source, identityGradientTransform)
	return cc
}

func identityGradientTransform(point models.StPos) canvas.Point {
	return canvas.Point{X: point.X, Y: point.Y}
}

// setColor 设置普通颜色。没有颜色值时保持当前绘制状态。
func (p *Document) setColor(set func(color.Color), source *models.CTColor) {
	if source != nil && source.Value != nil {
		set(ofdColorRGBA(*source))
	}
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
func getLineJoin(joinStr string) canvas.Joiner {
	switch joinStr {
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
	fill, stroke := p.updateDrawParams(ctx, dp)
	effective := *object
	if effective.LineWidth == 0 {
		if dp != nil && dp.LineWidth != 0 {
			effective.LineWidth = dp.LineWidth
		} else {
			effective.LineWidth = 0.353
		}
	}
	if effective.Cap == "" && dp != nil {
		effective.Cap = dp.Cap
	}
	if effective.Join == "" && dp != nil {
		effective.Join = dp.Join
	}
	if effective.MiterLimit == 0 && dp != nil {
		effective.MiterLimit = dp.MiterLimit
	}
	if effective.DashPattern == nil && dp != nil {
		effective.DashPattern = dp.DashPattern
		effective.DashOffset = dp.DashOffset
	}

	if object.FillColor != nil {
		fill = p.updateCtColor(object.FillColor)
	}
	if object.Fill {
		p.applyFill(ctx, fill, object.Alpha)
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
		ctx.SetStrokeWidth(effective.LineWidth)
		p.applyStroke(ctx, stroke, &effective)
	} else {
		ctx.SetStrokeWidth(-1)
	}

	if effective.DashPattern != nil {
		ctx.SetDashes(effective.DashOffset, *effective.DashPattern...)
	}
}

func (p *Document) applyFill(ctx *canvas.Context, fill *CTColor, alpha *uint8) {
	if fill == nil {
		ctx.SetFillColor(canvas.Transparent)
		return
	}
	if fill.Value != nil {
		value := *fill.Value
		if alpha != nil {
			value.A = uint8(uint16(value.A) * uint16(*alpha) / 255)
		}
		ctx.SetFillColor(value)
		return
	}
	if fill.Gradient != nil {
		ctx.SetFillGradient(fill.Gradient)
		return
	}
	ctx.SetFillColor(canvas.Transparent)
}

func (p *Document) applyStroke(ctx *canvas.Context, stroke *CTColor, object *models.CtPath) {
	if stroke == nil || (stroke.Value == nil && stroke.Gradient == nil) {
		ctx.SetStrokeColor(canvas.Black)
	} else if stroke.Gradient != nil {
		ctx.SetStrokeGradient(stroke.Gradient)
	} else {
		ctx.SetStrokeColor(*stroke.Value)
	}
	ctx.SetStrokeCapper(getLineCap(object.Cap))
	joiner := getLineJoin(object.Join)
	if joiner == canvas.MiterJoin {
		miterLimit := object.MiterLimit
		if miterLimit == 0 {
			miterLimit = 3.528
		}
		// OFD 将 MiterLimit 定义为以毫米为单位的绝对长度。
		// Canvas 使用相对于中心线测量的斜接长度进行比较，
		// 因此这里需要换算为相对于半线宽的倍率。
		lineWidth := ctx.Style.StrokeWidth
		if lineWidth > 0 {
			miterLimit /= lineWidth / 2
		}
		joiner = canvas.MiterJoiner{GapJoiner: canvas.BevelJoin, Limit: miterLimit}
	}
	ctx.SetStrokeJoiner(joiner)
}
