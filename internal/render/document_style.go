package render

import (
	"image/color"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/pkg/spec"
)

func (p *Document) updateDrawParams(ctx DrawContext, dp *models.DrawParam) (*CTColor, *CTColor) {
	if dp == nil {
		return nil, nil
	}
	p.updateDrawParamStyle(ctx, dp)
	return p.updateCtColor(dp.FillColor), p.updateCtColor(dp.StrokeColor)
}

func (p *Document) updateDrawParamStyle(ctx DrawContext, dp *models.DrawParam) {
	if dp == nil {
		return
	}
	if dp.StrokeColor != nil {
		p.setColor(ctx.SetStrokeColor, dp.StrokeColor)
	}
	ctx.SetStrokeWidth(paint.NormalizeLineWidth(dp.LineWidth))
	dashPattern := paint.PrepareDashPattern(dp.DashPattern, dp.Cap, 1)
	if paint.ValidDashPattern(dp.DashOffset, dashPattern) {
		ctx.SetDashes(dp.DashOffset, *dashPattern...)
	} else {
		ctx.SetDashes(0)
	}
	ctx.SetStrokeCapper(paint.LineCapper(dp.Cap))
	ctx.SetStrokeJoiner(paint.LineJoiner(dp.Join))
}

// setColor 设置普通颜色。没有颜色值时保持当前绘制状态。
func (p *Document) setColor(set func(color.Color), source *models.CTColor) {
	if source != nil && (source.Value != nil || source.ColorSpace != 0 || source.Index != 0) {
		set(p.colorRGBA(*source))
	}
}

// updateCtPathStyle 将 paint 解析的有效路径样式应用到绘制上下文。
func (p *Document) updateCtPathStyle(ctx DrawContext, object *models.CtPath, dp *models.DrawParam, strokeScale float64) models.CTGraphicUnit {
	if object == nil {
		return models.CTGraphicUnit{}
	}
	p.updateDrawParamStyle(ctx, dp)
	effective := paint.ResolvePathStyle(*object, dp, strokeScale)
	if object.Fill {
		p.applyFill(ctx, p.pathSolidColor(effective.FillColor), object.Alpha)
		if object.Rule == spec.FillRuleEvenOdd {
			ctx.SetFillRule(geom.EvenOdd)
		}
	} else {
		ctx.ClearFill()
	}
	if object.Stroke.Value(true) {
		ctx.SetStrokeWidth(effective.LineWidth)
		p.applyStroke(ctx, p.pathSolidColor(effective.StrokeColor), &effective)
	} else {
		ctx.ClearStroke()
	}
	if effective.DashPattern != nil {
		if paint.ValidDashPattern(effective.DashOffset, effective.DashPattern) {
			ctx.SetDashes(effective.DashOffset, *effective.DashPattern...)
		} else {
			ctx.SetDashes(0)
		}
	}
	return effective.CTGraphicUnit
}

// 路径渐变由 updatePathGradients 按 Boundary 坐标构建，这里仅解析普通颜色。
// 避免先构建一份无边界的网格/渐变，随后又被真正用于绘制的渐变覆盖。
func (p *Document) pathSolidColor(source *models.CTColor) *CTColor {
	return p.colors.ResolveSolid(source, p.colorResources())
}

func (p *Document) applyFill(ctx DrawContext, fill *CTColor, alpha *uint8) {
	if fill == nil {
		ctx.SetFillColor(geom.Transparent)
		return
	}
	if fill.HasValue {
		value := fill.Value
		if alpha != nil {
			value = paint.ScaleAlpha(value, graphicOpacity(alpha))
		}
		ctx.SetFillColor(value)
		return
	}
	if fill.Gradient != nil {
		ctx.SetFillGradient(fill.Gradient)
		return
	}
	ctx.SetFillColor(geom.Transparent)
}

// graphicOpacity 返回 OFD 图元/颜色 Alpha 对应的不透明度。
// OFD 的 Alpha 是不透明度：0 表示全透明，255 表示完全不透明，缺省 255。
func graphicOpacity(alpha *uint8) uint8 {
	if alpha == nil {
		return 255
	}
	return *alpha
}

func (p *Document) applyStroke(ctx DrawContext, stroke *CTColor, object *models.CtPath) {
	if stroke == nil || (!stroke.HasValue && stroke.Gradient == nil) {
		ctx.SetStrokeColor(paint.ScaleAlpha(geom.Black, graphicOpacity(object.Alpha)))
	} else if stroke.Gradient != nil {
		ctx.SetStrokeGradient(stroke.Gradient)
	} else {
		ctx.SetStrokeColor(paint.ScaleAlpha(stroke.Value, graphicOpacity(object.Alpha)))
	}
	ctx.SetStrokeCapper(paint.LineCapper(object.Cap))
	ctx.SetStrokeJoiner(paint.StrokeJoiner(object.Join, object.MiterLimit, ctx.StrokeWidth()))
}
