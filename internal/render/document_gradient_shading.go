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
	if g.mapType == "Repeat" || g.mapType == "Reflect" {
		if g.mapUnit <= 0 {
			return g.base.At(x, y)
		}
		dx, dy := x-g.c0.X, y-g.c0.Y
		distance := math.Sqrt(dx*dx+dy*dy) - g.r0
		return gradientColor(g.stops, mapGradientValue(distance/g.mapUnit, g.mapType), g.extend)
	}
	// Direct：按两圆插值族的参数 t 应用 Extend 位。一个点可能同时落在多个
	// 圆上，由 RadialParameter 依据 Extend 允许的根取较大的 t；没有任何可用
	// 解时颜色未定义，保持透明（与 PDF 径向着色一致，例如起始圆退化为焦点
	// 时焦点背面的区域）。
	t, ok := g.base.RadialParameter(x, y, g.extend)
	if !ok {
		return color.RGBA{}
	}
	return gradientColor(g.stops, t, g.extend)
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
	// 原生 LinearGradient 只在 [0,1] 之外夹取端点色，等价于 Extend=3；
	// Extend=0/1/2 的路径渐变走 ofdLinearGradient（矢量后端会栅格化，
	// 避免 SVG 无法表达单向延伸时铺满轴外区域）。文字另见 textShadingGradient。
	if shd.MapType != "Repeat" && shd.MapType != "Reflect" && shd.Extend == 3 {
		gradient := geom.NewLinearGradient(start, end)
		addOFDGradientStops(&gradient.Grad, shd.Segment, resolve)
		gradient.Extend = extendBits(shd.Extend)
		return gradient
	}

	gradient := &ofdLinearGradient{
		start:   start,
		end:     end,
		mapType: shd.MapType,
		mapUnit: shd.MapUnit,
		extend:  shd.Extend,
	}
	gradient.dx = end.X - start.X
	gradient.dy = end.Y - start.Y
	gradient.d2 = gradient.dx*gradient.dx + gradient.dy*gradient.dy
	gradient.unit = math.Sqrt(gradient.d2)
	addOFDGradientStops(&gradient.stops, shd.Segment, resolve)
	return gradient
}

// extendBits 把 OFD 的 Extend 位（第 0 位起点侧、第 1 位终点侧）转为两端标志。
func extendBits(extend int) [2]bool {
	return [2]bool{extend&1 != 0, extend&2 != 0}
}

// textShadingGradient 为矢量后端的文字构造可写成 PDF 原生 Shading 的渐变：
// Direct 轴向渐变与非焦点径向渐变转为带 Extend 位的原生 Linear/RadialGradient，
// 从而让文字保留真实文本 + 原生 /Shading（PDF 中可复制）。Repeat/Reflect、
// 椭圆、焦点径向等无法原生表达时返回 nil，由调用方走栅格化。
func (p *Document) textShadingGradient(source *models.CTColor) geom.Gradient {
	if source == nil {
		return nil
	}
	if shd := source.AxialShd; shd != nil {
		if shd.MapType == "Repeat" || shd.MapType == "Reflect" {
			return nil
		}
		start := identityGradientTransform(shd.StartPoint)
		end := identityGradientTransform(shd.EndPoint)
		if !finitePoint(start) || !finitePoint(end) || !finiteFloat(shd.MapUnit) {
			return nil
		}
		gradient := geom.NewLinearGradient(start, end)
		addOFDGradientStops(&gradient.Grad, shd.Segment, p.colorRGBA)
		gradient.Extend = extendBits(shd.Extend)
		return gradient
	}
	if shd := source.RadialShd; shd != nil {
		if shd.MapType == "Repeat" || shd.MapType == "Reflect" || shd.Eccentricity > 0 || shd.Angle != 0 {
			return nil
		}
		c0 := identityGradientTransform(shd.StartPoint)
		c1 := identityGradientTransform(shd.EndPoint)
		if !finitePoint(c0) || !finitePoint(c1) || !finiteFloat(shd.StartRadius) || !finiteFloat(shd.EndRadius) {
			return nil
		}
		// 焦点径向（起始圆退化为点且与终止圆不同心）需要逐点判定焦点背面，
		// 原生渐变会错误地铺上起点色。
		if geom.Equal(shd.StartRadius, 0) && c0 != c1 {
			return nil
		}
		gradient := geom.NewRadialGradient(c0, shd.StartRadius, c1, shd.EndRadius)
		addOFDGradientStops(&gradient.Grad, shd.Segment, p.colorRGBA)
		gradient.Extend = extendBits(shd.Extend)
		return gradient
	}
	return nil
}

// isAxialRadialShading 判断颜色是否使用轴向或径向着色渐变。
func isAxialRadialShading(source *models.CTColor) bool {
	return source != nil && (source.AxialShd != nil || source.RadialShd != nil)
}

// radialMapUnit 返回 Repeat/Reflect 的区间长度。MapUnit 省略时用起止半径差，
// 使终止圆之外继续按该区间平铺或反射，而不是退回 Direct。
func radialMapUnit(shd *models.CTRadialShd) float64 {
	if shd.MapUnit > 0 {
		return shd.MapUnit
	}
	span := math.Abs(shd.EndRadius - shd.StartRadius)
	if span == 0 {
		return 0
	}
	return span
}

func newOFDRadialGradient(shd *models.CTRadialShd, transform func(models.StPos) geom.Point, resolve colorResolver) geom.Gradient {
	c0 := transform(shd.StartPoint)
	c1 := transform(shd.EndPoint)
	if !finitePoint(c0) || !finitePoint(c1) || !finiteFloat(shd.StartRadius) || !finiteFloat(shd.EndRadius) || !finiteFloat(shd.Eccentricity) || !finiteFloat(shd.Angle) || !finiteFloat(shd.MapUnit) || shd.Eccentricity >= 1 {
		return nil
	}
	hasElliptical := shd.Eccentricity > 0 || shd.Angle != 0
	hasMapType := shd.MapType == "Repeat" || shd.MapType == "Reflect"

	// 原生 RadialGradient 只在 [0,1] 之外夹取端点色，仅等价于 Extend=3；
	// Extend=0/1/2 的路径渐变按 t 逐点判定（矢量后端会栅格化，避免 SVG 铺满）。
	// 起始圆退化为焦点的偏心渐变也走 ofdRadialGradient：焦点背面的根半径为负，
	// 原生夹取会错误地铺上起点色。文字另见 textShadingGradient。
	focal := geom.Equal(shd.StartRadius, 0) && c0 != c1
	if !hasElliptical {
		if !hasMapType && shd.Extend == 3 && !focal {
			gradient := geom.NewRadialGradient(c0, shd.StartRadius, c1, shd.EndRadius)
			addOFDGradientStops(&gradient.Grad, shd.Segment, resolve)
			return gradient
		}
		mapUnit := 0.0
		if hasMapType {
			mapUnit = radialMapUnit(shd)
		}
		gradient := &ofdRadialGradient{
			c0:      c0,
			r0:      shd.StartRadius,
			c1:      c1,
			r1:      shd.EndRadius,
			mapType: shd.MapType,
			mapUnit: mapUnit,
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
		mapUnit:      radialMapUnit(shd),
		extend:       shd.Extend,
		hasMapType:   hasMapType,
	}
	if hasMapType {
		gradient.stops = base.Grad
	}
	return gradient
}
