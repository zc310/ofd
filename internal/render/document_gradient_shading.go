package render

import (
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// 本文件实现轴向/径向/椭圆轴向渐变（ofdRadialGradient/ofdEllipticalGradient）
// 及其工厂 newOFDLinearGradient/newOFDRadialGradient。

func (g *ofdRadialGradient) At(x, y float64) color.RGBA {
	if g.mapUnit <= 0 || (g.mapType != "Repeat" && g.mapType != "Reflect") {
		return g.base.At(x, y)
	}
	dx, dy := x-g.c0.X, y-g.c0.Y
	distance := math.Sqrt(dx*dx+dy*dy) - g.r0
	return gradientColor(g.stops, mapGradientValue(distance/g.mapUnit, g.mapType), g.extend)
}

// ofdEllipticalGradient 支持椭圆径向渐变（Eccentricity/Angle）。
type ofdEllipticalGradient struct {
	base         *geom.RadialGradient
	stops        geom.Grad
	c0           geom.Point
	r0           float64
	c1           geom.Point
	r1           float64
	eccentricity float64
	angle        float64
	mapType      string
	mapUnit      float64
	extend       int
	hasMapType   bool
}

func (g *ofdEllipticalGradient) At(x, y float64) color.RGBA {
	cx := (g.c0.X + g.c1.X) / 2
	cy := (g.c0.Y + g.c1.Y) / 2
	rx := x - cx
	ry := y - cy

	cosA := math.Cos(-g.angle)
	sinA := math.Sin(-g.angle)
	rotX := rx*cosA - ry*sinA
	rotY := rx*sinA + ry*cosA

	if g.eccentricity > 0 {
		scaleY := 1.0 / math.Sqrt(1-g.eccentricity*g.eccentricity)
		rotY *= scaleY
	}

	ax := rotX + cx
	ay := rotY + cy

	if g.hasMapType && g.mapUnit > 0 {
		dx, dy := ax-g.c0.X, ay-g.c0.Y
		distance := math.Sqrt(dx*dx+dy*dy) - g.r0
		return gradientColor(g.stops, mapGradientValue(distance/g.mapUnit, g.mapType), g.extend)
	}
	return g.base.At(ax, ay)
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

func newOFDLinearGradient(shd *models.CTAxialShd, transform func(models.StPos) geom.Point, resolve colorResolver) geom.Gradient {
	start := transform(shd.StartPoint)
	end := transform(shd.EndPoint)
	if !finitePoint(start) || !finitePoint(end) || !finiteFloat(shd.MapUnit) {
		return nil
	}
	if shd.MapType != "Repeat" && shd.MapType != "Reflect" {
		gradient := geom.NewLinearGradient(start, end)
		addOFDGradientStops(&gradient.Grad, shd.Segment, resolve)
		return gradient
	}

	gradient := &ofdLinearGradient{
		start:   start,
		end:     end,
		mapType: shd.MapType,
		mapUnit: shd.MapUnit,
		extend:  shd.Extend,
	}
	addOFDGradientStops(&gradient.stops, shd.Segment, resolve)
	return gradient
}

func newOFDRadialGradient(shd *models.CTRadialShd, transform func(models.StPos) geom.Point, resolve colorResolver) geom.Gradient {
	c0 := transform(shd.StartPoint)
	c1 := transform(shd.EndPoint)
	if !finitePoint(c0) || !finitePoint(c1) || !finiteFloat(shd.StartRadius) || !finiteFloat(shd.EndRadius) || !finiteFloat(shd.Eccentricity) || !finiteFloat(shd.Angle) || !finiteFloat(shd.MapUnit) || shd.Eccentricity >= 1 {
		return nil
	}
	hasElliptical := shd.Eccentricity > 0 || shd.Angle != 0
	hasMapType := shd.MapType == "Repeat" || shd.MapType == "Reflect"

	if !hasElliptical && !hasMapType {
		gradient := geom.NewRadialGradient(c0, shd.StartRadius, c1, shd.EndRadius)
		addOFDGradientStops(&gradient.Grad, shd.Segment, resolve)
		return gradient
	}

	if !hasElliptical && hasMapType {
		gradient := &ofdRadialGradient{
			c0:      c0,
			r0:      shd.StartRadius,
			c1:      c1,
			r1:      shd.EndRadius,
			mapType: shd.MapType,
			mapUnit: shd.MapUnit,
			extend:  shd.Extend,
		}
		addOFDGradientStops(&gradient.stops, shd.Segment, resolve)
		gradient.base = geom.NewRadialGradient(gradient.c0, gradient.r0, gradient.c1, gradient.r1)
		gradient.base.Grad = gradient.stops
		return gradient
	}

	base := geom.NewRadialGradient(c0, shd.StartRadius, c1, shd.EndRadius)
	addOFDGradientStops(&base.Grad, shd.Segment, resolve)
	gradient := &ofdEllipticalGradient{
		base:         base,
		c0:           c0,
		r0:           shd.StartRadius,
		c1:           c1,
		r1:           shd.EndRadius,
		eccentricity: shd.Eccentricity,
		angle:        shd.Angle * math.Pi / 180,
		mapType:      shd.MapType,
		mapUnit:      shd.MapUnit,
		extend:       shd.Extend,
		hasMapType:   hasMapType,
	}
	if hasMapType {
		gradient.stops = base.Grad
	}
	return gradient
}
