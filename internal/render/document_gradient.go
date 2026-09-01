package render

import (
	"image/color"
	"math"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

// ofdLinearGradient 实现 OFD 轴向渐变映射模式，特别是
// canvas.LinearGradient 不直接支持的 Repeat 和 Reflect 模式。
type ofdLinearGradient struct {
	stops   canvas.Grad
	start   canvas.Point
	end     canvas.Point
	mapType string
	mapUnit float64
	extend  int
}

func (g *ofdLinearGradient) SetColorSpace(colorSpace canvas.ColorSpace) canvas.Gradient {
	return g
}

func (g *ofdLinearGradient) At(x, y float64) color.RGBA {
	dx, dy := g.end.X-g.start.X, g.end.Y-g.start.Y
	d2 := dx*dx + dy*dy
	if d2 == 0 {
		return g.stops.At(0)
	}
	projection := ((x-g.start.X)*dx + (y-g.start.Y)*dy) / math.Sqrt(d2)
	unit := math.Sqrt(d2)
	if g.mapUnit > 0 && (g.mapType == "Repeat" || g.mapType == "Reflect") {
		return g.stops.At(mapGradientValue(projection/g.mapUnit, g.mapType))
	}
	return gradientColor(g.stops, projection/unit, g.extend)
}

// ofdRadialGradient 将普通径向插值交给 canvas 处理，存在时再根据 OFD
// 的 Repeat 或 Reflect 模式映射径向距离。
type ofdRadialGradient struct {
	stops   canvas.Grad
	c0      canvas.Point
	r0      float64
	c1      canvas.Point
	r1      float64
	mapType string
	mapUnit float64
	extend  int
	base    *canvas.RadialGradient
}

func (g *ofdRadialGradient) SetColorSpace(colorSpace canvas.ColorSpace) canvas.Gradient {
	return g
}

func (g *ofdRadialGradient) At(x, y float64) color.RGBA {
	if g.mapUnit <= 0 || (g.mapType != "Repeat" && g.mapType != "Reflect") {
		return g.base.At(x, y)
	}
	dx, dy := x-g.c0.X, y-g.c0.Y
	distance := math.Sqrt(dx*dx+dy*dy) - g.r0
	return gradientColor(g.stops, mapGradientValue(distance/g.mapUnit, g.mapType), g.extend)
}

func mapGradientValue(value float64, mapType string) float64 {
	switch mapType {
	case "Repeat":
		value -= math.Floor(value)
		return value
	case "Reflect":
		value = math.Mod(value, 2)
		if value < 0 {
			value += 2
		}
		if value > 1 {
			return 2 - value
		}
		return value
	default:
		return value
	}
}

func newOFDLinearGradient(shd *models.CTAxialShd, transform func(models.StPos) canvas.Point) canvas.Gradient {
	gradient := &ofdLinearGradient{
		start:   transform(shd.StartPoint),
		end:     transform(shd.EndPoint),
		mapType: shd.MapType,
		mapUnit: shd.MapUnit,
		extend:  shd.Extend,
	}
	addOFDGradientStops(&gradient.stops, shd.Segment)
	return gradient
}

func newOFDRadialGradient(shd *models.CTRadialShd, transform func(models.StPos) canvas.Point) canvas.Gradient {
	gradient := &ofdRadialGradient{
		c0:      transform(shd.StartPoint),
		r0:      shd.StartRadius,
		c1:      transform(shd.EndPoint),
		r1:      shd.EndRadius,
		mapType: shd.MapType,
		mapUnit: shd.MapUnit,
		extend:  shd.Extend,
	}
	addOFDGradientStops(&gradient.stops, shd.Segment)
	gradient.base = canvas.NewRadialGradient(gradient.c0, gradient.r0, gradient.c1, gradient.r1)
	gradient.base.Grad = gradient.stops
	return gradient
}

func addOFDGradientStops(gradient *canvas.Grad, segments []models.Segment) {
	if len(segments) == 0 {
		return
	}
	allPositionsOmitted := true
	for _, segment := range segments {
		if segment.Position != 0 {
			allPositionsOmitted = false
			break
		}
	}
	for i, segment := range segments {
		position := segment.Position
		// OFD 允许省略位置。常见的双色标形式表示渐变的起点和终点，
		// 而不是两个都位于 0 的色标。
		if allPositionsOmitted && len(segments) > 1 {
			position = float64(i) / float64(len(segments)-1)
		}
		if segment.Color.Value != nil {
			value := segment.Color.Value.RGBA
			if segment.Color.Alpha != nil {
				value.A = uint8(uint16(value.A) * uint16(255-*segment.Color.Alpha) / 255)
			}
			gradient.Add(position, value)
		}
	}
}

// gradientColor 在调用 canvas.Grad 插值前应用 OFD 的 Extend 语义。
// 第 0 位表示延伸起始颜色，第 1 位表示延伸结束颜色。
func gradientColor(stops canvas.Grad, position float64, extend int) color.RGBA {
	if position < 0 && extend&1 == 0 {
		return color.RGBA{}
	}
	if position > 1 && extend&2 == 0 {
		return color.RGBA{}
	}
	return stops.At(position)
}
