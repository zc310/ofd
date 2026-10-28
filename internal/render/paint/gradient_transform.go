package paint

import (
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/render/geom"
)

// translatedGradient 将页面坐标系中的渐变适配到局部栅格画布，
// 同时保持渐变的视觉起点不变。
type translatedGradient struct {
	geom.Gradient

	dx, dy float64
}

// TranslateGradient 将页面渐变适配到原点位于 (dx,dy) 的局部画布。
func TranslateGradient(gradient geom.Gradient, dx, dy float64) geom.Gradient {
	if gradient == nil || (dx == 0 && dy == 0) {
		return gradient
	}
	return &translatedGradient{Gradient: gradient, dx: dx, dy: dy}
}

func (g *translatedGradient) At(x, y float64) color.RGBA {
	return g.Gradient.At(x+g.dx, y+g.dy)
}

// TextRunGradient 把 Boundary 空间渐变映射到文字 run 的字体空间。
// origin 使用 Boundary 空间的毫米坐标；字体空间以基线为原点，Y 向上。
// 调用方负责将 HScale 计入 run 原点位移，本函数只缩放字形内的横坐标。
func TextRunGradient(gradient geom.Gradient, origin geom.Point, hScale float64) geom.Gradient {
	return textRunGradient{gradient: gradient, origin: origin, hScale: hScale}
}

type textRunGradient struct {
	gradient geom.Gradient
	origin   geom.Point
	hScale   float64
}

func (g textRunGradient) At(x, y float64) color.RGBA {
	return g.gradient.At(x*g.hScale+g.origin.X, g.origin.Y-y)
}

// DeviceGradient 将 Boundary 空间渐变适配到已烘焙变换的设备空间轮廓。
// matrix 将 Boundary 坐标映射到设备坐标；原矩阵、逆矩阵非有限，
// 或行列式非有限及为零时返回 false。
func DeviceGradient(gradient geom.Gradient, matrix geom.Matrix) (geom.Gradient, bool) {
	det := matrix.Det()
	if !finiteMatrix(matrix) || !finiteFloat(det) || geom.Equal(det, 0.0) {
		return nil, false
	}
	inverse := matrix.Inv()
	if !finiteMatrix(inverse) {
		return nil, false
	}
	return textDeviceGradient{gradient: gradient, inverse: inverse}, true
}

type textDeviceGradient struct {
	gradient geom.Gradient
	inverse  geom.Matrix
}

func (g textDeviceGradient) At(x, y float64) color.RGBA {
	point := g.inverse.Dot(geom.Point{X: x, Y: y})
	return g.gradient.At(point.X, point.Y)
}

// MapNativeGradient 将 Boundary 渐变换算到局部空间，matrix 将局部坐标映射到 Boundary。
// 返回独立的原生渐变并共享只读色标；几何无法精确表达时返回 nil，供调用方走采样回退。
func MapNativeGradient(gradient geom.Gradient, matrix geom.Matrix) geom.Gradient {
	det := matrix.Det()
	if !finiteMatrix(matrix) || !finiteFloat(det) || geom.Equal(det, 0) {
		return nil
	}
	inverse := matrix.Inv()
	if !finiteMatrix(inverse) {
		return nil
	}
	switch g := gradient.(type) {
	case *geom.LinearGradient:
		start := inverse.Dot(g.Start)
		d := g.End.Sub(g.Start)
		// 参数 t=(M·p-start)·d/|d|²；用转置变换等色线法向，避免倾斜及
		// 非等比缩放时直接变换两个端点造成插值方向错误。
		normal := geom.Point{X: matrix[0][0]*d.X + matrix[1][0]*d.Y, Y: matrix[0][1]*d.X + matrix[1][1]*d.Y}
		end := start
		if n2 := normal.Dot(normal); n2 > 0 {
			end = start.Add(normal.Mul(d.Dot(d) / n2))
		}
		if !finitePoint(start) || !finitePoint(end) {
			return nil
		}
		mapped := geom.NewLinearGradient(start, end)
		mapped.Grad, mapped.Extend = g.Grad, g.Extend
		return mapped
	case *geom.RadialGradient:
		// 非等比变换把圆变成椭圆，不能再用两个圆的原生 RadialGradient 表达。
		if !matrix.IsSimilarity() {
			return nil
		}
		scale := math.Sqrt(math.Abs(inverse.Det()))
		c0, c1 := inverse.Dot(g.C0), inverse.Dot(g.C1)
		r0, r1 := g.R0*scale, g.R1*scale
		if !finitePoint(c0) || !finitePoint(c1) || !finiteFloat(r0) || !finiteFloat(r1) {
			return nil
		}
		mapped := geom.NewRadialGradient(c0, r0, c1, r1)
		mapped.Grad, mapped.Extend = g.Grad, g.Extend
		return mapped
	}
	return nil
}

func finiteMatrix(matrix geom.Matrix) bool {
	for _, row := range matrix {
		for _, value := range row {
			if !finiteFloat(value) {
				return false
			}
		}
	}
	return true
}
