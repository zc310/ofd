package paint

import "github.com/zc310/ofd/internal/render/geom"

// NeedsRaster 判断路径渐变是否需要栅格化以保留采样及透明度语义。
// canvas 的 PDF 着色只写 DeviceRGB，无法保留色标 Alpha；SVG 也不能表达
// 路径渐变的单向延伸。Repeat/Reflect、椭圆和焦点径向因此使用采样路径。
func NeedsRaster(gradient geom.Gradient, objectAlpha *uint8) bool {
	if gradient == nil {
		return false
	}
	if graphicOpacity(objectAlpha) < 255 || gradientHasTransparency(gradient) {
		return true
	}
	switch value := gradient.(type) {
	case *geom.LinearGradient:
		return false
	case *geom.RadialGradient:
		return geom.Equal(value.R0, 0) && value.C0 != value.C1
	default:
		// 采样适配器、网格及其他自定义渐变都不能序列化为原生 Shading。
		return true
	}
}

func gradientHasTransparency(gradient geom.Gradient) bool {
	var stops geom.Grad
	switch value := gradient.(type) {
	case *geom.LinearGradient:
		stops = value.Grad
	case *geom.RadialGradient:
		stops = value.Grad
	case *ofdLinearGradient:
		stops = value.stops
	case *ofdRadialGradient:
		stops = value.stops
	case *ofdEllipticalGradient:
		stops = value.stops
	default:
		return false
	}
	for _, stop := range stops {
		if stop.Color.A != 255 {
			return true
		}
	}
	return false
}
