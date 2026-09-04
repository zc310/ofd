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

// ofdMeshGradient 实现 Gouraud 着色使用的三角网格渐变。
// canvas 只提供按位置采样的渐变接口，因此这里使用重心坐标直接对网格采样。
type ofdMeshGradient struct {
	triangles []ofdMeshTriangle
	backColor color.RGBA
	extend    bool
}

type ofdMeshTriangle struct {
	p0, p1, p2 canvas.Point
	c0, c1, c2 color.RGBA
}

type ofdOpacityGradient struct {
	canvas.Gradient
	opacity uint8
}

func (g *ofdOpacityGradient) SetColorSpace(colorSpace canvas.ColorSpace) canvas.Gradient {
	g.Gradient = g.Gradient.SetColorSpace(colorSpace)
	return g
}

func (g *ofdOpacityGradient) At(x, y float64) color.RGBA {
	value := g.Gradient.At(x, y)
	value.R = uint8(uint16(value.R) * uint16(g.opacity) / 255)
	value.G = uint8(uint16(value.G) * uint16(g.opacity) / 255)
	value.B = uint8(uint16(value.B) * uint16(g.opacity) / 255)
	value.A = uint8(uint16(value.A) * uint16(g.opacity) / 255)
	return value
}

func scaleGradientOpacity(gradient canvas.Gradient, opacity uint8) canvas.Gradient {
	if gradient == nil || opacity == 255 {
		return gradient
	}
	scaleStops := func(stops canvas.Grad) canvas.Grad {
		result := make(canvas.Grad, len(stops))
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
	case *canvas.LinearGradient:
		copy := *value
		copy.Grad = scaleStops(value.Grad)
		return &copy
	case *canvas.RadialGradient:
		copy := *value
		copy.Grad = scaleStops(value.Grad)
		return &copy
	default:
		return &ofdOpacityGradient{Gradient: gradient, opacity: opacity}
	}
}

func (g *ofdMeshGradient) SetColorSpace(colorSpace canvas.ColorSpace) canvas.Gradient {
	return g
}

func (g *ofdMeshGradient) At(x, y float64) color.RGBA {
	point := canvas.Point{X: x, Y: y}
	for _, triangle := range g.triangles {
		w0, w1, w2, ok := triangleWeights(point, triangle.p0, triangle.p1, triangle.p2)
		if ok {
			return interpolateMeshColor(triangle.c0, triangle.c1, triangle.c2, w0, w1, w2)
		}
	}
	if g.extend {
		return g.backColor
	}
	return color.RGBA{}
}

func triangleWeights(point, p0, p1, p2 canvas.Point) (float64, float64, float64, bool) {
	denominator := (p1.Y-p2.Y)*(p0.X-p2.X) + (p2.X-p1.X)*(p0.Y-p2.Y)
	if math.Abs(denominator) < 1e-12 {
		return 0, 0, 0, false
	}

	w0 := ((p1.Y-p2.Y)*(point.X-p2.X) + (p2.X-p1.X)*(point.Y-p2.Y)) / denominator
	w1 := ((p2.Y-p0.Y)*(point.X-p2.X) + (p0.X-p2.X)*(point.Y-p2.Y)) / denominator
	w2 := 1 - w0 - w1
	const epsilon = 1e-9
	return w0, w1, w2, w0 >= -epsilon && w1 >= -epsilon && w2 >= -epsilon
}

// interpolateMeshColor 按 canvas.Grad 的预乘 RGBA 方式插值，确保半透明网格顶点
// 与普通渐变使用相同的颜色表示。
func interpolateMeshColor(c0, c1, c2 color.RGBA, w0, w1, w2 float64) color.RGBA {
	r0, g0, b0, a0 := c0.RGBA()
	r1, g1, b1, a1 := c1.RGBA()
	r2, g2, b2, a2 := c2.RGBA()
	return color.RGBA{
		R: uint8((w0*float64(r0) + w1*float64(r1) + w2*float64(r2)) / 257),
		G: uint8((w0*float64(g0) + w1*float64(g1) + w2*float64(g2)) / 257),
		B: uint8((w0*float64(b0) + w1*float64(b1) + w2*float64(b2)) / 257),
		A: uint8((w0*float64(a0) + w1*float64(a1) + w2*float64(a2)) / 257),
	}
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

// ofdEllipticalGradient 支持椭圆径向渐变（Eccentricity/Angle）。
type ofdEllipticalGradient struct {
	base         *canvas.RadialGradient
	stops        canvas.Grad
	c0           canvas.Point
	r0           float64
	c1           canvas.Point
	r1           float64
	eccentricity float64
	angle        float64
	mapType      string
	mapUnit      float64
	extend       int
	hasMapType   bool
}

func (g *ofdEllipticalGradient) SetColorSpace(colorSpace canvas.ColorSpace) canvas.Gradient {
	return g
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

func newOFDLinearGradient(shd *models.CTAxialShd, transform func(models.StPos) canvas.Point) canvas.Gradient {
	start := transform(shd.StartPoint)
	end := transform(shd.EndPoint)
	if shd.MapType != "Repeat" && shd.MapType != "Reflect" {
		gradient := canvas.NewLinearGradient(start, end)
		addOFDGradientStops(&gradient.Grad, shd.Segment)
		return gradient
	}

	gradient := &ofdLinearGradient{
		start:   start,
		end:     end,
		mapType: shd.MapType,
		mapUnit: shd.MapUnit,
		extend:  shd.Extend,
	}
	addOFDGradientStops(&gradient.stops, shd.Segment)
	return gradient
}

func newOFDRadialGradient(shd *models.CTRadialShd, transform func(models.StPos) canvas.Point) canvas.Gradient {
	c0 := transform(shd.StartPoint)
	c1 := transform(shd.EndPoint)
	hasElliptical := shd.Eccentricity > 0 || shd.Angle != 0
	hasMapType := shd.MapType == "Repeat" || shd.MapType == "Reflect"

	if !hasElliptical && !hasMapType {
		gradient := canvas.NewRadialGradient(c0, shd.StartRadius, c1, shd.EndRadius)
		addOFDGradientStops(&gradient.Grad, shd.Segment)
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
		addOFDGradientStops(&gradient.stops, shd.Segment)
		gradient.base = canvas.NewRadialGradient(gradient.c0, gradient.r0, gradient.c1, gradient.r1)
		gradient.base.Grad = gradient.stops
		return gradient
	}

	base := canvas.NewRadialGradient(c0, shd.StartRadius, c1, shd.EndRadius)
	addOFDGradientStops(&base.Grad, shd.Segment)
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

// newOFDGouraudGradient 创建自由三角网格。
// EdgeFlag=0 开始新的三角形，1 和 2 按 OFD 自由网格格式复用前一个三角形的对应边。
func newOFDGouraudGradient(shd *models.CTGouraudShd, transform func(models.StPos) canvas.Point) canvas.Gradient {
	if shd == nil {
		return nil
	}
	triangles := make([]ofdMeshTriangle, 0)
	var previous [3]ofdMeshVertex
	hasPrevious := false
	for index := 0; index < len(shd.Point); {
		point := shd.Point[index]
		if !hasPrevious || point.EdgeFlag == 0 {
			if index+2 >= len(shd.Point) {
				break
			}
			previous = [3]ofdMeshVertex{
				newGouraudVertex(shd.Point[index], transform),
				newGouraudVertex(shd.Point[index+1], transform),
				newGouraudVertex(shd.Point[index+2], transform),
			}
			triangles = append(triangles, makeMeshTriangle(previous[0], previous[1], previous[2]))
			hasPrevious = true
			index += 3
			continue
		}

		if point.EdgeFlag != 1 && point.EdgeFlag != 2 {
			index++
			continue
		}
		vertex := newGouraudVertex(point, transform)
		if point.EdgeFlag == 1 {
			previous = [3]ofdMeshVertex{previous[1], previous[2], vertex}
		} else {
			previous = [3]ofdMeshVertex{previous[0], previous[2], vertex}
		}
		triangles = append(triangles, makeMeshTriangle(previous[0], previous[1], previous[2]))
		index++
	}
	return newMeshGradient(triangles, shd.BackColor, shd.Extend != 0)
}

// newOFDLaGouraudGradient 创建规则网格形式的 Gouraud 渐变。
// 每个相邻的四个顶点沿对角线拆分为两个三角形。
func newOFDLaGouraudGradient(shd *models.CTLaGouraudShd, transform func(models.StPos) canvas.Point) canvas.Gradient {
	if shd == nil || shd.VerticesPerRow < 2 || len(shd.Point) < shd.VerticesPerRow*2 {
		return nil
	}
	rows := len(shd.Point) / shd.VerticesPerRow
	triangles := make([]ofdMeshTriangle, 0, (rows-1)*(shd.VerticesPerRow-1)*2)
	for row := 0; row < rows-1; row++ {
		for column := 0; column < shd.VerticesPerRow-1; column++ {
			topLeft := newLaGouraudVertex(shd.Point[row*shd.VerticesPerRow+column], transform)
			topRight := newLaGouraudVertex(shd.Point[row*shd.VerticesPerRow+column+1], transform)
			bottomLeft := newLaGouraudVertex(shd.Point[(row+1)*shd.VerticesPerRow+column], transform)
			bottomRight := newLaGouraudVertex(shd.Point[(row+1)*shd.VerticesPerRow+column+1], transform)
			triangles = append(triangles,
				makeMeshTriangle(topLeft, topRight, bottomLeft),
				makeMeshTriangle(topRight, bottomLeft, bottomRight),
			)
		}
	}
	return newMeshGradient(triangles, shd.BackColor, shd.Extend != 0)
}

type ofdMeshVertex struct {
	point canvas.Point
	color color.RGBA
}

func newGouraudVertex(point models.GouraudPoint, transform func(models.StPos) canvas.Point) ofdMeshVertex {
	return ofdMeshVertex{
		point: transform(models.StPos{X: point.X, Y: point.Y}),
		color: meshColor(point.Color),
	}
}

func newLaGouraudVertex(point models.LaGouraudPoint, transform func(models.StPos) canvas.Point) ofdMeshVertex {
	return ofdMeshVertex{
		point: transform(models.StPos{X: point.X, Y: point.Y}),
		color: meshColor(point.Color),
	}
}

func makeMeshTriangle(p0, p1, p2 ofdMeshVertex) ofdMeshTriangle {
	return ofdMeshTriangle{p0: p0.point, p1: p1.point, p2: p2.point, c0: p0.color, c1: p1.color, c2: p2.color}
}

func newMeshGradient(triangles []ofdMeshTriangle, backColor *models.CTColor, extend bool) canvas.Gradient {
	gradient := &ofdMeshGradient{
		triangles: triangles,
		backColor: color.RGBA{A: 255},
		extend:    extend,
	}
	if backColor != nil {
		gradient.backColor = meshColor(*backColor)
	}
	return gradient
}

func meshColor(source models.CTColor) color.RGBA {
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
	value.A = alpha
	return value
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
		gradient.Add(position, ofdColorRGBA(segment.Color))
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
