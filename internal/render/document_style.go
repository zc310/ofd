package render

import (
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

const (
	defaultLineWidth  = 0.353
	defaultMiterLimit = 3.528
	maxStrokeValue    = 1000000
	maxDashElements   = 1024
)

func (p *Document) updateDrawParams(ctx DrawContext, dp *models.DrawParam) (*CTColor, *CTColor) {
	if dp == nil {
		return nil, nil
	}

	if dp.StrokeColor != nil {
		p.setColor(ctx.SetStrokeColor, dp.StrokeColor)
	}
	lineWidth := dp.LineWidth
	lineWidth = normalizedLineWidth(lineWidth)
	ctx.SetStrokeWidth(lineWidth)
	if validDashPattern(dp.DashOffset, dp.DashPattern) {
		ctx.SetDashes(dp.DashOffset, *dp.DashPattern...)
	} else {
		ctx.SetDashes(0)
	}

	ctx.SetStrokeCapper(getLineCap(dp.Cap))
	ctx.SetStrokeJoiner(getLineJoin(dp.Join))

	return p.updateCtColor(dp.FillColor), p.updateCtColor(dp.StrokeColor)
}

func (p *Document) updateCtColor(source *models.CTColor) *CTColor {
	if source == nil {
		return nil
	}
	cc := &CTColor{}
	if source.Value != nil || source.ColorSpace != 0 || source.Index != 0 {
		cc.Value = p.colorRGBA(*source)
		cc.HasValue = true
	}

	cc.Gradient = p.pathGradient(source, identityGradientTransform)
	return cc
}

func identityGradientTransform(point models.StPos) geom.Point {
	return geom.Point{X: point.X, Y: point.Y}
}

// setColor 设置普通颜色。没有颜色值时保持当前绘制状态。
func (p *Document) setColor(set func(color.Color), source *models.CTColor) {
	if source != nil && (source.Value != nil || source.ColorSpace != 0 || source.Index != 0) {
		set(p.colorRGBA(*source))
	}
}

func getLineCap(capStr string) geom.Capper {
	switch capStr {
	case "Round":
		return geom.RoundCap
	case "Square":
		return geom.SquareCap
	default:
		return geom.ButtCap
	}
}
func getLineJoin(joinStr string) geom.Joiner {
	switch joinStr {
	case "Round":
		return geom.RoundJoin
	case "Bevel":
		return geom.BevelJoin
	default:
		return geom.MiterJoin
	}
}
func (p *Document) updateCtPathStyle(ctx DrawContext, object *models.CtPath, dp *models.DrawParam) {
	if object == nil {
		return
	}
	fill, stroke := p.updateDrawParams(ctx, dp)
	effective := *object
	if effective.LineWidth == 0 {
		if dp != nil && dp.LineWidth != 0 {
			effective.LineWidth = dp.LineWidth
		} else {
			effective.LineWidth = defaultLineWidth
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
			ctx.SetFillRule(geom.EvenOdd)
		}
	} else {
		ctx.ClearFill()
	}

	if object.StrokeColor != nil {
		stroke = p.updateCtColor(object.StrokeColor)
	}
	if object.Stroke.Value(true) {
		effective.LineWidth = normalizedLineWidth(effective.LineWidth)
		effective.MiterLimit = normalizedMiterLimit(effective.MiterLimit)
		ctx.SetStrokeWidth(effective.LineWidth)
		p.applyStroke(ctx, stroke, &effective)
	} else {
		ctx.SetStrokeWidth(-1)
	}

	if effective.DashPattern != nil {
		if validDashPattern(effective.DashOffset, effective.DashPattern) {
			ctx.SetDashes(effective.DashOffset, *effective.DashPattern...)
		} else {
			ctx.SetDashes(0)
		}
	}
}

func (p *Document) applyFill(ctx DrawContext, fill *CTColor, alpha *uint8) {
	if fill == nil {
		ctx.SetFillColor(geom.Transparent)
		return
	}
	if fill.HasValue {
		value := fill.Value
		if alpha != nil {
			value = scaleAlpha(value, graphicOpacity(alpha))
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
		ctx.SetStrokeColor(geom.Black)
	} else if stroke.Gradient != nil {
		ctx.SetStrokeGradient(stroke.Gradient)
	} else {
		ctx.SetStrokeColor(stroke.Value)
	}
	ctx.SetStrokeCapper(getLineCap(object.Cap))
	joiner := getLineJoin(object.Join)
	if _, isMiter := joiner.(geom.MiterJoiner); isMiter {
		miterLimit := object.MiterLimit
		miterLimit = normalizedMiterLimit(miterLimit)
		// OFD 将 MiterLimit 定义为以毫米为单位的绝对长度。
		// Canvas 使用相对于中心线测量的斜接长度进行比较，
		// 因此这里需要换算为相对于半线宽的倍率。
		lineWidth := ctx.StrokeWidth()
		if lineWidth > 0 {
			miterLimit /= lineWidth / 2
		}
		joiner = geom.MiterJoiner{GapJoiner: geom.BevelJoin, Limit: miterLimit}
	}
	ctx.SetStrokeJoiner(joiner)
}

func normalizedLineWidth(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > maxStrokeValue {
		return defaultLineWidth
	}
	return value
}

func normalizedMiterLimit(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > maxStrokeValue {
		return defaultMiterLimit
	}
	return value
}

func validDashPattern(offset float64, pattern *models.StArrayF) bool {
	if pattern == nil || len(*pattern) == 0 || len(*pattern) > maxDashElements ||
		offset < 0 || math.IsNaN(offset) || math.IsInf(offset, 0) || offset > maxStrokeValue {
		return false
	}
	hasLength := false
	for _, value := range *pattern {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > maxStrokeValue {
			return false
		}
		if value > 0 {
			hasLength = true
		}
	}
	return hasLength
}
