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
	Gradient canvas.Gradient
}

func (p *Document) updateCtColor(source *models.CTColor) *CTColor {
	if source == nil {
		return nil
	}
	cc := &CTColor{}
	if source.Value != nil {
		value := source.Value.RGBA
		// 颜色透明度，在 0~255 之间取值。默认为 255，表示完可选全不透明
		if source.Alpha != nil && *source.Alpha < 255 {
			value.A = 255 - *source.Alpha
		}
		cc.Value = &value
	}

	if source.AxialShd != nil {
		cc.Gradient = p.linearGradient(source.AxialShd)
	}
	if source.RadialShd != nil {
		cc.Gradient = p.radialGradient(source.RadialShd)
	}
	return cc
}

// linearGradient 创建轴向渐变。
func (p *Document) linearGradient(shd *models.CTAxialShd) canvas.Gradient {
	return newOFDLinearGradient(shd, func(point models.StPos) canvas.Point {
		return canvas.Point{X: point.X, Y: point.Y}
	})
}

// radialGradient 创建径向渐变。
func (p *Document) radialGradient(shd *models.CTRadialShd) canvas.Gradient {
	return newOFDRadialGradient(shd, func(point models.StPos) canvas.Point {
		return canvas.Point{X: point.X, Y: point.Y}
	})
}

// setColor 设置普通颜色。没有颜色值时保持当前绘制状态。
func (p *Document) setColor(set func(color.Color), source *models.CTColor) {
	if source != nil && source.Value != nil {
		set(source.Value.RGBA)
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
		ctx.SetStrokeWidth(max(object.LineWidth, 1) * 0.353)
		p.applyStroke(ctx, stroke, object)
	} else {
		ctx.SetStrokeWidth(-1)
	}

	if object.DashPattern != nil {
		ctx.SetDashes(object.DashOffset, *object.DashPattern...)
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
			value.A = 255 - *alpha
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
		if object.MiterLimit == 0 {
			object.MiterLimit = 3.528
		}
		joiner = canvas.MiterJoiner{GapJoiner: canvas.BevelJoin, Limit: object.MiterLimit}
	}
	ctx.SetStrokeJoiner(joiner)
}
