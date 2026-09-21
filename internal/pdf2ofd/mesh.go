package pdf2ofd

import (
	"bytes"
	"image"
	"image/png"
	"math"

	"github.com/zc310/ofd/pkg/creator"
)

// 网格着色（ShadingType 4/5/6/7）优先输出为 OFD 矢量网格：自由网格（Type 4）
// 输出 GouraudShd，规则网格（Type 5）输出 LaGouraudShd，补丁网格（Type 6/7）
// 细分后输出 GouraudShd。只有当控制点数量超过上限、坐标非法或裁剪区无法还原
// 为路径时，才回退到带透明度的位图（meshImage），保证视觉不丢失。
//
// pdfMeshData 保存 ShadingType 4/5/6/7 网格着色的原始数据与解码参数。
type pdfMeshData struct {
	kind              int
	bitsPerCoordinate int
	bitsPerComponent  int
	bitsPerFlag       int
	verticesPerRow    int
	decode            []float64
	data              []byte
}

// maxMeshTriangles 限制单个网格着色展开的三角形数量，避免超大网格生成
// 难以处理的 OFD 文件。
const maxMeshTriangles = 300000

type meshBitReader struct {
	data []byte
	pos  int
}

func (r *meshBitReader) read(bits int) (uint32, bool) {
	if bits <= 0 {
		return 0, true
	}
	if r.pos+bits > len(r.data)*8 {
		return 0, false
	}
	var value uint32
	for i := 0; i < bits; i++ {
		value = value<<1 | uint32((r.data[r.pos/8]>>(7-uint(r.pos%8)))&1)
		r.pos++
	}
	return value, true
}

func (m *pdfMeshData) maxCoordinate() float64 {
	return float64(uint64(1)<<uint(m.bitsPerCoordinate) - 1)
}

func (m *pdfMeshData) maxComponent() float64 {
	if m.bitsPerComponent <= 0 {
		return 1
	}
	return float64(uint64(1)<<uint(m.bitsPerComponent) - 1)
}

func (m *pdfMeshData) decodeCoordinate(raw uint32, index int) float64 {
	low, high := 0.0, 1.0
	if 2*index+1 < len(m.decode) {
		low, high = m.decode[2*index], m.decode[2*index+1]
	}
	return low + float64(raw)/m.maxCoordinate()*(high-low)
}

func (m *pdfMeshData) decodeComponent(raw uint32, index int) float64 {
	low, high := 0.0, 1.0
	if 4+2*index+1 < len(m.decode) {
		low, high = m.decode[4+2*index], m.decode[4+2*index+1]
	}
	return low + float64(raw)/m.maxComponent()*(high-low)
}

type meshVertex struct {
	x, y   float64
	color  pdfColor
	flag   int
	usable bool
}

// decodeMeshTriangles 把网格着色解析为三角形集合（坐标位于着色空间）。
func decodeMeshTriangles(shading *pdfShading) [][3]meshVertex {
	if shading == nil || shading.mesh == nil || shading.colorSpace == nil || len(shading.mesh.data) == 0 {
		return nil
	}
	mesh := shading.mesh
	reader := &meshBitReader{data: mesh.data}
	switch mesh.kind {
	case 4:
		return decodeFreeFormMesh(mesh, reader, shading)
	case 5:
		return decodeLatticeMesh(mesh, reader, shading)
	case 6, 7:
		return decodePatchMesh(mesh, reader, shading)
	default:
		return nil
	}
}

// meshTriangleMM 是页面毫米坐标下带顶点颜色的三角形。
type meshTriangleMM struct {
	x [3]float64
	y [3]float64
	c [3]pdfColor
}

// maxVectorMeshPoints 限制单个网格以矢量方式输出时的控制点数量。超过该上限
// 时回退为位图，避免生成过大的 OFD 文件与过慢的阅读器渲染。
const maxVectorMeshPoints = 1 << 16

// ofdMeshPoint 是位于着色空间、准备写入 OFD 的网格控制点。
type ofdMeshPoint struct {
	x, y  float64
	color pdfColor
	flag  int
}

// decodeMeshPoints 把网格着色解码为 OFD 控制点。自由网格与补丁网格返回按
// 三角形排列的点（使用 EdgeFlag，可为零），规则网格返回行优先排列的点并附
// 每行顶点数。
func decodeMeshPoints(shading *pdfShading) (points []ofdMeshPoint, verticesPerRow int, ok bool) {
	if shading == nil || shading.mesh == nil || shading.colorSpace == nil || len(shading.mesh.data) == 0 {
		return nil, 0, false
	}
	mesh := shading.mesh
	reader := &meshBitReader{data: mesh.data}
	switch mesh.kind {
	case 4:
		points, ok = decodeFreeFormPoints(mesh, reader, shading)
		return points, 0, ok
	case 5:
		return decodeLatticePoints(mesh, reader, shading)
	case 6, 7:
		triangles := decodePatchMesh(mesh, reader, shading)
		if len(triangles) == 0 {
			return nil, 0, false
		}
		points = make([]ofdMeshPoint, 0, len(triangles)*3)
		for _, triangle := range triangles {
			for _, vertex := range triangle {
				points = append(points, ofdMeshPoint{x: vertex.x, y: vertex.y, color: vertex.color})
			}
		}
		return points, 0, true
	default:
		return nil, 0, false
	}
}

// decodeFreeFormPoints 解析自由网格（Type 4），保留 EdgeFlag 以便阅读器重建
// 与原始网格一致的三角形邻接关系。
func decodeFreeFormPoints(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading) ([]ofdMeshPoint, bool) {
	components := shading.colorSpace.components
	var points []ofdMeshPoint
	for len(points) < maxVectorMeshPoints {
		flag, ok := reader.read(maxInt(mesh.bitsPerFlag, 2))
		if !ok {
			break
		}
		vertex, ok := readMeshVertex(mesh, reader, components, shading, int(flag))
		if !ok {
			break
		}
		if flag == 0 {
			second, ok := readMeshVertex(mesh, reader, components, shading, 1)
			if !ok {
				break
			}
			third, ok := readMeshVertex(mesh, reader, components, shading, 2)
			if !ok {
				break
			}
			points = append(points,
				ofdMeshPoint{x: vertex.x, y: vertex.y, color: vertex.color, flag: 0},
				ofdMeshPoint{x: second.x, y: second.y, color: second.color, flag: 1},
				ofdMeshPoint{x: third.x, y: third.y, color: third.color, flag: 2})
			continue
		}
		points = append(points, ofdMeshPoint{x: vertex.x, y: vertex.y, color: vertex.color, flag: int(flag)})
	}
	if len(points) < 3 {
		return nil, false
	}
	return points, true
}

// decodeLatticePoints 解析规则网格（Type 5），返回行优先的控制点与每行顶点数。
func decodeLatticePoints(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading) ([]ofdMeshPoint, int, bool) {
	components := shading.colorSpace.components
	perRow := mesh.verticesPerRow
	if perRow < 2 {
		return nil, 0, false
	}
	var points []ofdMeshPoint
	for len(points) < maxVectorMeshPoints {
		row := make([]ofdMeshPoint, 0, perRow)
		incomplete := false
		for i := 0; i < perRow; i++ {
			vertex, ok := readMeshVertex(mesh, reader, components, shading, 0)
			if !ok {
				incomplete = true
				break
			}
			row = append(row, ofdMeshPoint{x: vertex.x, y: vertex.y, color: vertex.color})
		}
		if incomplete {
			break
		}
		points = append(points, row...)
	}
	if len(points) < perRow*2 {
		return nil, 0, false
	}
	return points, perRow, true
}

// meshVectorColor 把网格着色转换为 OFD 矢量网格填充。path 提供局部坐标原点，
// 与其它渐变一样，控制点坐标相对路径边界左上角。
func (p *pdfInterpreter) meshVectorColor(shading *pdfShading, matrix [6]float64, path *creator.Path) (*creator.Color, bool) {
	points, perRow, ok := decodeMeshPoints(shading)
	if !ok || len(points) == 0 || len(points) > maxVectorMeshPoints {
		return nil, false
	}
	toLocal := func(x, y float64) (float64, float64) {
		deviceX, deviceY := transformPDFPoint(x, y, matrix)
		pageX, pageY := p.pagePoint(deviceX, deviceY)
		return pageX - path.X, pageY - path.Y
	}
	backColor, extend := p.meshBackColor(shading)
	// 规则网格（Type 5）输出 LaGouraudShd，控制点更少也更贴近原始插值。
	if perRow >= 2 && len(points)%perRow == 0 && len(points) >= perRow*2 {
		result := &creator.LaGouraudShading{
			VerticesPerRow: perRow,
			Extend:         extend,
			BackColor:      backColor,
			Points:         make([]creator.LaGouraudPoint, 0, len(points)),
		}
		for _, point := range points {
			x, y := toLocal(point.x, point.y)
			result.Points = append(result.Points, creator.LaGouraudPoint{
				X: x, Y: y,
				Color: creator.Color{R: point.color.r, G: point.color.g, B: point.color.b},
			})
		}
		return &creator.Color{LaGouraud: result}, true
	}
	result := &creator.GouraudShading{
		Extend:    extend,
		BackColor: backColor,
		Points:    make([]creator.GouraudPoint, 0, len(points)),
	}
	for _, point := range points {
		x, y := toLocal(point.x, point.y)
		result.Points = append(result.Points, creator.GouraudPoint{
			X: x, Y: y, EdgeFlag: point.flag,
			Color: creator.Color{R: point.color.r, G: point.color.g, B: point.color.b},
		})
	}
	return &creator.Color{Gouraud: result}, true
}

// meshBackColor 把网格着色的 Background 转换为 OFD BackColor，并启用延展。
func (p *pdfInterpreter) meshBackColor(shading *pdfShading) (*creator.Color, int) {
	if !shading.hasMeshBackground {
		return nil, 0
	}
	color := shading.meshBackground
	return &creator.Color{R: color.r, G: color.g, B: color.b}, 1
}

// meshImage 是矢量网格输出的回退路径：当控制点超过上限或裁剪区无法还原为路径
// 时，把网格着色软光栅化为带透明度的 PNG 位图，三角形之外的像素保持透明。
func (p *pdfInterpreter) meshImage(shading *pdfShading, matrix [6]float64) *creator.Image {
	triangles := decodeMeshTriangles(shading)
	if len(triangles) == 0 {
		return nil
	}
	converted := make([]meshTriangleMM, 0, len(triangles))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, triangle := range triangles {
		var item meshTriangleMM
		valid := true
		for index, vertex := range triangle {
			deviceX, deviceY := transformPDFPoint(vertex.x, vertex.y, matrix)
			pageX, pageY := p.pagePoint(deviceX, deviceY)
			if math.IsNaN(pageX) || math.IsInf(pageX, 0) || math.IsNaN(pageY) || math.IsInf(pageY, 0) {
				valid = false
				break
			}
			item.x[index], item.y[index] = pageX, pageY
			item.c[index] = vertex.color
			minX, minY = math.Min(minX, pageX), math.Min(minY, pageY)
			maxX, maxY = math.Max(maxX, pageX), math.Max(maxY, pageY)
		}
		if valid {
			converted = append(converted, item)
		}
	}
	if math.IsInf(minX, 1) || !(maxX > minX) || !(maxY > minY) {
		return nil
	}
	imageData := rasterizeMeshTriangles(converted, minX, minY, maxX, maxY)
	if imageData == nil {
		return nil
	}
	img := &creator.Image{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY, Data: imageData, Format: "PNG"}
	img.Alpha = ofdTransparency(p.fillOpacity())
	if clips := p.buildClips(minX, minY, true, maxX-minX, maxY-minY); clips != nil {
		img.Clips = clips
	}
	return img
}

// meshRasterPixelsPerMM 是网格光栅化的像素密度（约 152 DPI）。
const meshRasterPixelsPerMM = 6.0

// maxMeshRasterPixels 限制网格位图的像素总数。
const maxMeshRasterPixels = 8 << 20

// rasterizeMeshTriangles 用重心坐标对三角形做 Gouraud 光栅化，返回 PNG 数据。
func rasterizeMeshTriangles(triangles []meshTriangleMM, minX, minY, maxX, maxY float64) []byte {
	width := int(math.Ceil((maxX - minX) * meshRasterPixelsPerMM))
	height := int(math.Ceil((maxY - minY) * meshRasterPixelsPerMM))
	if width*height > maxMeshRasterPixels {
		scale := math.Sqrt(float64(maxMeshRasterPixels) / float64(width*height))
		width = int(float64(width) * scale)
		height = int(float64(height) * scale)
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	scaleX := float64(width) / (maxX - minX)
	scaleY := float64(height) / (maxY - minY)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for _, triangle := range triangles {
		x0, y0 := (triangle.x[0]-minX)*scaleX, (triangle.y[0]-minY)*scaleY
		x1, y1 := (triangle.x[1]-minX)*scaleX, (triangle.y[1]-minY)*scaleY
		x2, y2 := (triangle.x[2]-minX)*scaleX, (triangle.y[2]-minY)*scaleY
		area := (x1-x0)*(y2-y0) - (x2-x0)*(y1-y0)
		if math.Abs(area) < 1e-9 {
			continue
		}
		loX := int(math.Floor(math.Min(x0, math.Min(x1, x2))))
		hiX := int(math.Ceil(math.Max(x0, math.Max(x1, x2))))
		loY := int(math.Floor(math.Min(y0, math.Min(y1, y2))))
		hiY := int(math.Ceil(math.Max(y0, math.Max(y1, y2))))
		if loX < 0 {
			loX = 0
		}
		if loY < 0 {
			loY = 0
		}
		if hiX >= width {
			hiX = width - 1
		}
		if hiY >= height {
			hiY = height - 1
		}
		c0, c1, c2 := triangle.c[0], triangle.c[1], triangle.c[2]
		for py := loY; py <= hiY; py++ {
			fy := float64(py) + 0.5
			for px := loX; px <= hiX; px++ {
				fx := float64(px) + 0.5
				w0 := ((x1-fx)*(y2-fy) - (x2-fx)*(y1-fy)) / area
				w1 := ((x2-fx)*(y0-fy) - (x0-fx)*(y2-fy)) / area
				w2 := 1 - w0 - w1
				if w0 < -1e-6 || w1 < -1e-6 || w2 < -1e-6 {
					continue
				}
				offset := img.PixOffset(px, py)
				img.Pix[offset] = meshBlend(c0.r, c1.r, c2.r, w0, w1, w2)
				img.Pix[offset+1] = meshBlend(c0.g, c1.g, c2.g, w0, w1, w2)
				img.Pix[offset+2] = meshBlend(c0.b, c1.b, c2.b, w0, w1, w2)
				img.Pix[offset+3] = 255
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil
	}
	return encoded.Bytes()
}

func meshBlend(c0, c1, c2 uint8, w0, w1, w2 float64) uint8 {
	value := float64(c0)*w0 + float64(c1)*w1 + float64(c2)*w2
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return uint8(value + 0.5)
}

func decodeFreeFormMesh(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading) [][3]meshVertex {
	components := shading.colorSpace.components
	var previous [3]meshVertex
	hasPrevious := false
	var triangles [][3]meshVertex
	for len(triangles) < maxMeshTriangles {
		flag, ok := reader.read(maxInt(mesh.bitsPerFlag, 2))
		if !ok {
			break
		}
		vertex, ok := readMeshVertex(mesh, reader, components, shading, int(flag))
		if !ok {
			break
		}
		if !hasPrevious || flag == 0 {
			v1, ok1 := readMeshVertex(mesh, reader, components, shading, 1)
			v2, ok2 := readMeshVertex(mesh, reader, components, shading, 2)
			if !ok1 || !ok2 {
				break
			}
			previous = [3]meshVertex{vertex, v1, v2}
			hasPrevious = true
			triangles = append(triangles, previous)
			continue
		}
		if flag == 1 {
			previous = [3]meshVertex{previous[1], previous[2], vertex}
		} else {
			previous = [3]meshVertex{previous[0], previous[2], vertex}
		}
		triangles = append(triangles, previous)
	}
	return triangles
}

func decodeLatticeMesh(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading) [][3]meshVertex {
	components := shading.colorSpace.components
	perRow := mesh.verticesPerRow
	if perRow < 2 {
		return nil
	}
	var rows [][]meshVertex
	for len(rows)*perRow < maxMeshTriangles {
		row := make([]meshVertex, 0, perRow)
		for i := 0; i < perRow; i++ {
			vertex, ok := readMeshVertex(mesh, reader, components, shading, 0)
			if !ok {
				row = nil
				break
			}
			row = append(row, vertex)
		}
		if len(row) != perRow {
			break
		}
		rows = append(rows, row)
		// 至少两行才能生成三角形。
		if len(rows) >= 2 && reader.pos >= len(reader.data)*8 {
			break
		}
	}
	var triangles [][3]meshVertex
	for row := 0; row+1 < len(rows); row++ {
		for col := 0; col+1 < perRow; col++ {
			a := rows[row][col]
			b := rows[row][col+1]
			c := rows[row+1][col]
			d := rows[row+1][col+1]
			triangles = append(triangles, [3]meshVertex{a, b, c}, [3]meshVertex{b, d, c})
			if len(triangles) >= maxMeshTriangles {
				return triangles
			}
		}
	}
	return triangles
}

// decodePatchMesh 解析 Coons（Type 6）与 Tensor（Type 7）补丁网格。每个补丁按
// 双三次/Coons 曲面采样为三角形，颜色在四个角点颜色间双线性插值。
func decodePatchMesh(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading) [][3]meshVertex {
	components := shading.colorSpace.components
	pointCount := 12
	if mesh.kind == 7 {
		pointCount = 16
	}
	var triangles [][3]meshVertex
	for len(triangles) < maxMeshTriangles {
		flag, ok := reader.read(mesh.bitsPerFlag)
		if !ok {
			break
		}
		if flag != 0 {
			// 共享边的补丁暂不支持；停止以当前位置继续按“新补丁”解析会
			// 错位，因此直接返回已解析的部分。
			break
		}
		points := make([][2]float64, pointCount)
		valid := true
		for i := 0; i < pointCount; i++ {
			rawX, okX := reader.read(mesh.bitsPerCoordinate)
			rawY, okY := reader.read(mesh.bitsPerCoordinate)
			if !okX || !okY {
				valid = false
				break
			}
			points[i] = [2]float64{mesh.decodeCoordinate(rawX, 0), mesh.decodeCoordinate(rawY, 1)}
		}
		if !valid {
			break
		}
		colors := make([][3]float64, 4)
		for i := 0; i < 4; i++ {
			componentsValues := make([]float64, components)
			for c := 0; c < components; c++ {
				raw, ok := reader.read(mesh.bitsPerComponent)
				if !ok {
					return triangles
				}
				componentsValues[c] = mesh.decodeComponent(raw, c)
			}
			color, ok := shading.colorSpace.colorFromComponents(componentsValues)
			if !ok {
				return triangles
			}
			colors[i] = [3]float64{float64(color.r), float64(color.g), float64(color.b)}
		}
		if mesh.kind == 7 && len(points) >= 16 {
			// Adobe 生成的 Tensor 网格把控制点按 Coons 边界顺序（p0..p11）排列，
			// 4 个内部点 p12..p15 追加在末尾，因此角点为 p0/p3/p6/p9。
			triangles = append(triangles, tessellateCoonsPatch(points[:12], colors)...)
		} else {
			triangles = append(triangles, tessellateCoonsPatch(points, colors)...)
		}
	}
	return triangles
}

// tessellateCoonsPatch 把 12 控制点的 Coons 曲面采样为三角形。
func tessellateCoonsPatch(points [][2]float64, colors [][3]float64) [][3]meshVertex {
	const steps = 3
	sample := func(u, v float64) (float64, float64, pdfColor) {
		bottom := bezierPoint(points[0], points[1], points[2], points[3], u)
		top := bezierPoint(points[9], points[8], points[7], points[6], u)
		left := bezierPoint(points[0], points[11], points[10], points[9], v)
		right := bezierPoint(points[3], points[4], points[5], points[6], v)
		corners := [4][2]float64{points[0], points[3], points[6], points[9]}
		bilinearity := [2]float64{
			(1-u)*(1-v)*corners[0][0] + u*(1-v)*corners[1][0] + u*v*corners[2][0] + (1-u)*v*corners[3][0],
			(1-u)*(1-v)*corners[0][1] + u*(1-v)*corners[1][1] + u*v*corners[2][1] + (1-u)*v*corners[3][1],
		}
		x := (1-v)*bottom[0] + v*top[0] + (1-u)*left[0] + u*right[0] - bilinearity[0]
		y := (1-v)*bottom[1] + v*top[1] + (1-u)*left[1] + u*right[1] - bilinearity[1]
		return x, y, bilinearColor(colors, u, v)
	}
	return tessellateGrid(sample, steps)
}

func tessellateGrid(sample func(u, v float64) (float64, float64, pdfColor), steps int) [][3]meshVertex {
	vertices := make([][]meshVertex, steps+1)
	for i := 0; i <= steps; i++ {
		vertices[i] = make([]meshVertex, steps+1)
		for j := 0; j <= steps; j++ {
			x, y, color := sample(float64(i)/float64(steps), float64(j)/float64(steps))
			vertices[i][j] = meshVertex{x: x, y: y, color: color, usable: true}
		}
	}
	var triangles [][3]meshVertex
	for i := 0; i < steps; i++ {
		for j := 0; j < steps; j++ {
			a, b := vertices[i][j], vertices[i+1][j]
			c, d := vertices[i][j+1], vertices[i+1][j+1]
			triangles = append(triangles, [3]meshVertex{a, b, d}, [3]meshVertex{a, d, c})
		}
	}
	return triangles
}

func readMeshVertex(mesh *pdfMeshData, reader *meshBitReader, components int, shading *pdfShading, flag int) (meshVertex, bool) {
	rawX, okX := reader.read(mesh.bitsPerCoordinate)
	rawY, okY := reader.read(mesh.bitsPerCoordinate)
	if !okX || !okY {
		return meshVertex{}, false
	}
	values := make([]float64, components)
	for c := 0; c < components; c++ {
		raw, ok := reader.read(mesh.bitsPerComponent)
		if !ok {
			return meshVertex{}, false
		}
		values[c] = mesh.decodeComponent(raw, c)
	}
	color, ok := shading.colorSpace.colorFromComponents(values)
	if !ok {
		return meshVertex{}, false
	}
	return meshVertex{x: mesh.decodeCoordinate(rawX, 0), y: mesh.decodeCoordinate(rawY, 1), color: color, flag: flag, usable: true}, true
}

func bezierBasis(t float64) [4]float64 {
	mt := 1 - t
	return [4]float64{mt * mt * mt, 3 * t * mt * mt, 3 * t * t * mt, t * t * t}
}

func bezierPoint(p0, p1, p2, p3 [2]float64, t float64) [2]float64 {
	b := bezierBasis(t)
	return [2]float64{
		b[0]*p0[0] + b[1]*p1[0] + b[2]*p2[0] + b[3]*p3[0],
		b[0]*p0[1] + b[1]*p1[1] + b[2]*p2[1] + b[3]*p3[1],
	}
}

// bilinearColor 在四个角点颜色间双线性插值。colors 顺序为 (0,0)、(1,0)、
// (1,1)、(0,1)。
func bilinearColor(colors [][3]float64, u, v float64) pdfColor {
	c00, c10, c11, c01 := colors[0], colors[1], colors[2], colors[3]
	w00 := (1 - u) * (1 - v)
	w10 := u * (1 - v)
	w11 := u * v
	w01 := (1 - u) * v
	r := w00*c00[0] + w10*c10[0] + w11*c11[0] + w01*c01[0]
	g := w00*c00[1] + w10*c10[1] + w11*c11[1] + w01*c01[1]
	b := w00*c00[2] + w10*c10[2] + w11*c11[2] + w01*c01[2]
	clamp := func(value float64) uint8 {
		if value <= 0 {
			return 0
		}
		if value >= 255 {
			return 255
		}
		return uint8(math.Round(value))
	}
	return pdfColor{r: clamp(r), g: clamp(g), b: clamp(b)}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
