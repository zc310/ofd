package render

import (
	"image/color"
	"math"
	"sync"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// 本文件实现网格类渐变：ofdMeshGradient（三角形重心坐标采样 + 均匀网格
// 空间索引）及 newOFDGouraudGradient/newOFDLaGouraudGradient/newMeshGradient。

// ofdMeshGradient 实现 Gouraud 着色使用的三角网格渐变。
// geom 只提供按位置采样的渐变接口，因此这里使用重心坐标直接对网格采样。
//
// 为避免逐像素线性遍历全部三角形（O(像素×三角形)），首次采样时构建均匀网格
// 空间索引，把每个采样点限制在少数候选三角形内。跨度过大的三角形放入全局
// 列表，保证索引不膨胀且结果正确。
type ofdMeshGradient struct {
	triangles []ofdMeshTriangle
	backColor color.RGBA
	extend    bool
	// hull 是全部控制点的包围盒。控制点只定义插值锚点，并不限制着色范围，
	// 因此落在包围盒之外的采样点要先夹回盒内再取色。
	hull geom.Rect
	// area 是着色区域在渐变局部坐标下的范围（即对象 Boundary）。只有落在
	// area 之内的点才按边缘颜色延续；区域之外由 Extend 决定是否绘制。
	area *geom.Rect

	gridOnce sync.Once
	grid     *meshGrid
}

type ofdMeshTriangle struct {
	p0, p1, p2 geom.Point
	c0, c1, c2 color.RGBA
}

// meshGrid 是按均匀网格划分三角形的空间索引。cells 以行优先展平，保存三角形
// 下标；globals 保存跨越过多网格单元、无法有效索引的三角形。
type meshGrid struct {
	minX, minY float64
	cell       float64
	cols, rows int
	cells      [][]int32
	globals    []int32
}

// meshGridMaxCellsPerTriangle 限制单个三角形写入的网格单元数；超过时改放
// 全局列表，避免大三角形让索引膨胀。
const meshGridMaxCellsPerTriangle = 64

// newMeshGrid 为三角形集合构建空间索引；三角形很少时返回 nil，直接线性查找。
func newMeshGrid(triangles []ofdMeshTriangle) *meshGrid {
	count := len(triangles)
	if count < 16 {
		return nil
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, triangle := range triangles {
		for _, point := range [3]geom.Point{triangle.p0, triangle.p1, triangle.p2} {
			minX = math.Min(minX, point.X)
			minY = math.Min(minY, point.Y)
			maxX = math.Max(maxX, point.X)
			maxY = math.Max(maxY, point.Y)
		}
	}
	if !(maxX > minX) || !(maxY > minY) {
		return nil
	}
	side := int(math.Ceil(math.Sqrt(float64(count))))
	if side < 8 {
		side = 8
	}
	if side > 192 {
		side = 192
	}
	cell := math.Max(maxX-minX, maxY-minY) / float64(side)
	if cell <= 0 || math.IsInf(cell, 0) {
		return nil
	}
	cols := int(math.Ceil((maxX-minX)/cell)) + 1
	rows := int(math.Ceil((maxY-minY)/cell)) + 1
	if cols < 1 || rows < 1 || cols > 4096 || rows > 4096 {
		return nil
	}
	grid := &meshGrid{
		minX: minX, minY: minY, cell: cell,
		cols: cols, rows: rows,
		cells: make([][]int32, cols*rows),
	}
	for index, triangle := range triangles {
		triMinX := math.Min(triangle.p0.X, math.Min(triangle.p1.X, triangle.p2.X))
		triMaxX := math.Max(triangle.p0.X, math.Max(triangle.p1.X, triangle.p2.X))
		triMinY := math.Min(triangle.p0.Y, math.Min(triangle.p1.Y, triangle.p2.Y))
		triMaxY := math.Max(triangle.p0.Y, math.Max(triangle.p1.Y, triangle.p2.Y))
		column0 := grid.column(triMinX)
		column1 := grid.column(triMaxX)
		row0 := grid.row(triMinY)
		row1 := grid.row(triMaxY)
		if (column1-column0+1)*(row1-row0+1) > meshGridMaxCellsPerTriangle {
			grid.globals = append(grid.globals, int32(index))
			continue
		}
		for row := row0; row <= row1; row++ {
			base := row * cols
			for column := column0; column <= column1; column++ {
				cellIndex := base + column
				grid.cells[cellIndex] = append(grid.cells[cellIndex], int32(index))
			}
		}
	}
	return grid
}

func (g *meshGrid) column(x float64) int {
	value := int((x - g.minX) / g.cell)
	if value < 0 {
		return 0
	}
	if value >= g.cols {
		return g.cols - 1
	}
	return value
}

func (g *meshGrid) row(y float64) int {
	value := int((y - g.minY) / g.cell)
	if value < 0 {
		return 0
	}
	if value >= g.rows {
		return g.rows - 1
	}
	return value
}

// candidates 返回可能覆盖该点的三角形下标；返回 false 表示点落在网格范围外，
// 此时只需检查全局列表。
func (g *meshGrid) candidates(x, y float64) ([]int32, bool) {
	if x < g.minX || x > g.minX+g.cell*float64(g.cols) || y < g.minY || y > g.minY+g.cell*float64(g.rows) {
		return nil, false
	}
	return g.cells[g.row(y)*g.cols+g.column(x)], true
}

func (g *ofdMeshGradient) At(x, y float64) color.RGBA {
	point := geom.Point{X: x, Y: y}
	if value, ok := g.sampleAt(point); ok {
		return value
	}
	// 着色区域之内的控制点空白按边缘颜色延续，而不是留空。字形常常溢出控制点
	// 范围（例如基线贴近 Boundary 底边、渐变点未覆盖整个 Boundary），直接返回
	// 透明色会把字形下半截整段抹掉。区域之外仍由 Extend 决定。
	if g.insideArea(point) {
		if value, ok := g.sampleAt(g.clampToHull(point)); ok {
			return value
		}
	}
	if g.extend {
		return g.backColor
	}
	return color.RGBA{}
}

// insideArea 判断采样点是否位于着色区域内。
func (g *ofdMeshGradient) insideArea(point geom.Point) bool {
	return g.area != nil &&
		point.X >= g.area.X0 && point.X <= g.area.X1 &&
		point.Y >= g.area.Y0 && point.Y <= g.area.Y1
}

// clampToHull 把采样点夹到控制点包围盒内。
func (g *ofdMeshGradient) clampToHull(point geom.Point) geom.Point {
	clamped := point
	clamped.X = math.Min(math.Max(point.X, g.hull.X0), g.hull.X1)
	clamped.Y = math.Min(math.Max(point.Y, g.hull.Y0), g.hull.Y1)
	return clamped
}

// sampleAt 返回覆盖该点的三角形颜色；使用空间索引把候选三角形限制在少数几个。
func (g *ofdMeshGradient) sampleAt(point geom.Point) (color.RGBA, bool) {
	g.gridOnce.Do(func() { g.grid = newMeshGrid(g.triangles) })
	if g.grid == nil {
		return sampleMeshTriangles(g.triangles, point)
	}
	for _, index := range g.grid.globals {
		if value, ok := sampleMeshTriangle(g.triangles[index], point); ok {
			return value, true
		}
	}
	if indices, ok := g.grid.candidates(point.X, point.Y); ok {
		for _, index := range indices {
			if value, ok := sampleMeshTriangle(g.triangles[index], point); ok {
				return value, true
			}
		}
	}
	return color.RGBA{}, false
}

func sampleMeshTriangle(triangle ofdMeshTriangle, point geom.Point) (color.RGBA, bool) {
	w0, w1, w2, ok := triangleWeights(point, triangle.p0, triangle.p1, triangle.p2)
	if !ok {
		return color.RGBA{}, false
	}
	return interpolateMeshColor(triangle.c0, triangle.c1, triangle.c2, w0, w1, w2), true
}

func sampleMeshTriangles(triangles []ofdMeshTriangle, point geom.Point) (color.RGBA, bool) {
	for _, triangle := range triangles {
		if value, ok := sampleMeshTriangle(triangle, point); ok {
			return value, true
		}
	}
	return color.RGBA{}, false
}

func triangleWeights(point, p0, p1, p2 geom.Point) (float64, float64, float64, bool) {
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

// interpolateMeshColor 按 geom.Grad 的预乘 RGBA 方式插值，确保半透明网格顶点
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

func newOFDGouraudGradient(shd *models.CTGouraudShd, transform func(models.StPos) geom.Point, resolve colorResolver) geom.Gradient {
	return newOFDGouraudGradientArea(shd, transform, nil, resolve)
}

// newOFDGouraudGradientArea 与 newOFDGouraudGradient 相同，但额外给出着色区域，
// 使区域内未被控制点覆盖的部分按边缘颜色延续。
func newOFDGouraudGradientArea(shd *models.CTGouraudShd, transform func(models.StPos) geom.Point, area *geom.Rect, resolve colorResolver) geom.Gradient {
	if shd == nil {
		return nil
	}
	triangles := make([]ofdMeshTriangle, 0)
	var previous [3]ofdMeshVertex
	hasPrevious := false
	// 控制点每三个构成一个三角形；EdgeFlag 为 1/2 时本点与上一个三角形共享
	// 一条边，用前两个顶点替换掉新三角形中不再需要的顶点（见图 40）。
	for index := 0; index < len(shd.Point); {
		point := shd.Point[index]
		// 首个控制点，或 EdgeFlag=0（方向标志为 0）：另起一个三角形。
		if !hasPrevious || point.EdgeFlag == 0 {
			if index+2 >= len(shd.Point) {
				break
			}
			previous = [3]ofdMeshVertex{
				newGouraudVertex(shd.Point[index], transform, resolve),
				newGouraudVertex(shd.Point[index+1], transform, resolve),
				newGouraudVertex(shd.Point[index+2], transform, resolve),
			}
			triangles = append(triangles, makeMeshTriangle(previous[0], previous[1], previous[2]))
			hasPrevious = true
			index += 3
			continue
		}

		if point.EdgeFlag != 1 && point.EdgeFlag != 2 && point.EdgeFlag != 3 {
			index++
			continue
		}
		vertex := newGouraudVertex(point, transform, resolve)
		// 方向标志语义见 GB/T 33190 图 40「方向标志的控制作用」：
		//   1 → 复用上一个三角形的 V1-V2 边，拼成四边形；
		//   2 → 复用 V0-V2 边，拼成四边形。
		// 两者的共同点是新三角形与上一个三角形共享一条边，因此网格连续；
		// EdgeFlag=0 不共享边，两个三角形各自独立填充。
		switch point.EdgeFlag {
		case 1:
			previous = [3]ofdMeshVertex{previous[1], previous[2], vertex}
		case 3:
			// 规范只定义 0/1/2，但个别文件沿用 PDF 语义写出 EdgeFlag=3
			// （复用上一个三角形的 V0-V1 边），这里宽容处理避免丢三角形。
			previous = [3]ofdMeshVertex{previous[0], previous[1], vertex}
		default:
			previous = [3]ofdMeshVertex{previous[0], previous[2], vertex}
		}
		triangles = append(triangles, makeMeshTriangle(previous[0], previous[1], previous[2]))
		index++
	}
	return newMeshGradient(triangles, shd.BackColor, shd.Extend != 0, area, resolve)
}

// newOFDLaGouraudGradient 创建规则网格形式的 Gouraud 渐变。
// 每个相邻的四个顶点沿对角线拆分为两个三角形。
func newOFDLaGouraudGradient(shd *models.CTLaGouraudShd, transform func(models.StPos) geom.Point, resolve colorResolver) geom.Gradient {
	return newOFDLaGouraudGradientArea(shd, transform, nil, resolve)
}

// newOFDLaGouraudGradientArea 与 newOFDLaGouraudGradient 相同，但额外给出着色区域。
func newOFDLaGouraudGradientArea(shd *models.CTLaGouraudShd, transform func(models.StPos) geom.Point, area *geom.Rect, resolve colorResolver) geom.Gradient {
	if shd == nil || shd.VerticesPerRow < 2 || len(shd.Point) < shd.VerticesPerRow*2 {
		return nil
	}
	// 规则网格要求 Point 数量是 VerticesPerRow 的整数倍（逐行完整）。末尾
	// 出现半行说明数据损坏或装配错误，整体拒绝而不是静默丢弃不完整的尾行。
	if len(shd.Point)%shd.VerticesPerRow != 0 {
		return nil
	}
	rows := len(shd.Point) / shd.VerticesPerRow
	triangles := make([]ofdMeshTriangle, 0, (rows-1)*(shd.VerticesPerRow-1)*2)
	for row := 0; row < rows-1; row++ {
		for column := 0; column < shd.VerticesPerRow-1; column++ {
			topLeft := newLaGouraudVertex(shd.Point[row*shd.VerticesPerRow+column], transform, resolve)
			topRight := newLaGouraudVertex(shd.Point[row*shd.VerticesPerRow+column+1], transform, resolve)
			bottomLeft := newLaGouraudVertex(shd.Point[(row+1)*shd.VerticesPerRow+column], transform, resolve)
			bottomRight := newLaGouraudVertex(shd.Point[(row+1)*shd.VerticesPerRow+column+1], transform, resolve)
			triangles = append(triangles,
				makeMeshTriangle(topLeft, topRight, bottomLeft),
				makeMeshTriangle(topRight, bottomLeft, bottomRight),
			)
		}
	}
	return newMeshGradient(triangles, shd.BackColor, shd.Extend != 0, area, resolve)
}

type ofdMeshVertex struct {
	point geom.Point
	color color.RGBA
}

func newGouraudVertex(point models.GouraudPoint, transform func(models.StPos) geom.Point, resolve colorResolver) ofdMeshVertex {
	return ofdMeshVertex{
		point: transform(models.StPos{X: point.X, Y: point.Y}),
		color: meshColor(point.Color, resolve),
	}
}

func newLaGouraudVertex(point models.LaGouraudPoint, transform func(models.StPos) geom.Point, resolve colorResolver) ofdMeshVertex {
	return ofdMeshVertex{
		point: transform(models.StPos{X: point.X, Y: point.Y}),
		color: meshColor(point.Color, resolve),
	}
}

func makeMeshTriangle(p0, p1, p2 ofdMeshVertex) ofdMeshTriangle {
	return ofdMeshTriangle{p0: p0.point, p1: p1.point, p2: p2.point, c0: p0.color, c1: p1.color, c2: p2.color}
}

// newMeshGradient 组装网格渐变。area 是着色区域在渐变局部坐标下的范围，
// 为 nil 时不做边缘夹取，区域外的空白一律按 Extend 处理。
func newMeshGradient(triangles []ofdMeshTriangle, backColor *models.CTColor, extend bool, area *geom.Rect, resolve colorResolver) geom.Gradient {
	if len(triangles) == 0 {
		return nil
	}
	for _, triangle := range triangles {
		if !finitePoint(triangle.p0) || !finitePoint(triangle.p1) || !finitePoint(triangle.p2) {
			return nil
		}
	}
	gradient := &ofdMeshGradient{
		triangles: triangles,
		backColor: color.RGBA{A: 255},
		extend:    extend,
		area:      area,
	}
	points := make([]geom.Point, 0, len(triangles)*3)
	for _, triangle := range triangles {
		points = append(points, triangle.p0, triangle.p1, triangle.p2)
	}
	gradient.hull = geom.RectFromPoints(points...)
	if backColor != nil {
		gradient.backColor = meshColor(*backColor, resolve)
	}
	return gradient
}
