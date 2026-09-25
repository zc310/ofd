package render

import (
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// ofdLinearGradient 实现 OFD 轴向渐变映射模式，特别是
// geom.LinearGradient 不直接支持的 Repeat 和 Reflect 模式。
type ofdLinearGradient struct {
	stops   geom.Grad
	start   geom.Point
	end     geom.Point
	mapType string
	mapUnit float64
	extend  int
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

// ofdRadialGradient 将普通径向插值交给 geom.RadialGradient 处理，存在时再根据 OFD
// 的 Repeat 或 Reflect 模式映射径向距离。
type ofdRadialGradient struct {
	stops   geom.Grad
	c0      geom.Point
	r0      float64
	c1      geom.Point
	r1      float64
	mapType string
	mapUnit float64
	extend  int
	base    *geom.RadialGradient
}

func finitePoint(point geom.Point) bool {
	return finiteFloat(point.X) && finiteFloat(point.Y)
}

func meshColor(source models.CTColor, resolve colorResolver) color.RGBA {
	if resolve != nil {
		return resolve(source)
	}
	if source.Value == nil {
		// OFD 未指定 Value 时各通道为 0，颜色透明度默认是 255。
		return color.RGBA{A: 255}
	}
	return ofdColorRGBA(source)
}

// ofdColorRGBA 将 OFD 颜色透明度转换为 Go 颜色的 Alpha 通道。
// OFD 的 Alpha 是不透明度，255 表示完全不透明，0 表示完全透明。
func ofdColorRGBA(source models.CTColor) color.RGBA {
	if source.Value == nil {
		return color.RGBA{A: 255}
	}
	value := source.Value.RGBA
	alpha := value.A
	if source.Alpha != nil {
		alpha = uint8(uint16(alpha) * uint16(*source.Alpha) / 255)
	}
	return premultiplied(value.R, value.G, value.B, alpha)
}

func addOFDGradientStops(gradient *geom.Grad, segments []models.Segment, resolve colorResolver) {
	if len(segments) == 0 {
		return
	}
	positions := segmentPositions(segments)
	for i, segment := range segments {
		gradient.Add(positions[i], meshColor(segment.Color, resolve))
	}
}

// segmentPositions 补齐省略的渐变色标位置。全部省略时均匀分布在 [0,1]；
// 首段省略为 0、末段省略为 1，中间连续省略的色标在两侧已知位置之间均分。
func segmentPositions(segments []models.Segment) []float64 {
	positions := make([]float64, len(segments))
	known := make([]bool, len(segments))
	anySet := false
	for i, segment := range segments {
		if segment.PositionSet {
			positions[i] = segment.Position
			known[i] = true
			anySet = true
		}
	}
	if !anySet {
		if len(segments) == 1 {
			return positions
		}
		for i := range positions {
			positions[i] = float64(i) / float64(len(segments)-1)
		}
		return positions
	}
	if !known[0] {
		positions[0] = 0
		known[0] = true
	}
	if len(segments) > 1 && !known[len(segments)-1] {
		positions[len(segments)-1] = 1
		known[len(segments)-1] = true
	}
	for i := 0; i < len(segments); {
		if known[i] {
			i++
			continue
		}
		start := i - 1
		end := i
		for end < len(segments) && !known[end] {
			end++
		}
		span := float64(end - start)
		for j := start + 1; j < end; j++ {
			positions[j] = positions[start] + (positions[end]-positions[start])*float64(j-start)/span
		}
		i = end
	}
	return positions
}

// gradientColor 在调用 geom.Grad 插值前应用 OFD 的 Extend 语义。
// 第 0 位表示延伸起始颜色，第 1 位表示延伸结束颜色。
func gradientColor(stops geom.Grad, position float64, extend int) color.RGBA {
	if position < 0 && extend&1 == 0 {
		return color.RGBA{}
	}
	if position > 1 && extend&2 == 0 {
		return color.RGBA{}
	}
	return stops.At(position)
}

type ofdOpacityGradient struct {
	geom.Gradient
	opacity uint8
}

func (g *ofdOpacityGradient) At(x, y float64) color.RGBA {
	value := g.Gradient.At(x, y)
	value.R = uint8(uint16(value.R) * uint16(g.opacity) / 255)
	value.G = uint8(uint16(value.G) * uint16(g.opacity) / 255)
	value.B = uint8(uint16(value.B) * uint16(g.opacity) / 255)
	value.A = uint8(uint16(value.A) * uint16(g.opacity) / 255)
	return value
}

func scaleGradientOpacity(gradient geom.Gradient, opacity uint8) geom.Gradient {
	if gradient == nil || opacity == 255 {
		return gradient
	}
	scaleStops := func(stops geom.Grad) geom.Grad {
		result := make(geom.Grad, len(stops))
		for i, stop := range stops {
			stop.Color.R = uint8(uint16(stop.Color.R) * uint16(opacity) / 255)
			stop.Color.G = uint8(uint16(stop.Color.G) * uint16(opacity) / 255)
			stop.Color.B = uint8(uint16(stop.Color.B) * uint16(opacity) / 255)
			stop.Color.A = uint8(uint16(stop.Color.A) * uint16(opacity) / 255)
			result[i] = stop
		}
		return result
	}
	switch value := gradient.(type) {
	case *geom.LinearGradient:
		copy := *value
		copy.Grad = scaleStops(value.Grad)
		return &copy
	case *geom.RadialGradient:
		copy := *value
		copy.Grad = scaleStops(value.Grad)
		return &copy
	default:
		return &ofdOpacityGradient{Gradient: gradient, opacity: opacity}
	}
}
