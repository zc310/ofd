package pdf2ofd

import (
	"bytes"
	"image"
	"math"

	"github.com/zc310/ofd/internal/media"
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
	// streamComponents 是每个顶点流里编码的分量数。函数型网格着色编码的是
	// 函数输入（通常 1 个），不等于颜色空间分量数；按颜色空间分量数读会让
	// 位流错位，坐标与颜色一起错。
	streamComponents int
	data             []byte
}

// maxMeshTriangles 限制单个网格着色展开的三角形数量，避免超大网格生成
// 难以处理的 OFD 文件。
const maxMeshTriangles = 300000

// maxLatticeVerticesPerRow 限制 Type 5 网格每行声明的顶点数。解码时在读取
// 位流前按该值预分配行切片，必须与总三角形预算一起限制。
const maxLatticeVerticesPerRow = maxVectorMeshPoints

// patchTessellationSteps 是每个 Type 6/7 补丁在 u、v 方向的细分数。补丁是
// 双三次/Coons 曲面，细分越细越贴近原曲面；6 段在常规补丁尺寸下已看不出刻面，
// 单个补丁 72 个三角形，配合 maxMeshTriangles 仍受上限保护。
const patchTessellationSteps = 6

type meshBitReader struct {
	data    []byte
	pos     int
	control *conversionControl
}

func (r *meshBitReader) read(bits int) (uint32, bool) {
	if r.control.consumeOperations(1) != nil {
		return 0, false
	}
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

// align 把读取位置推进到下一个字节边界。Type 6/7 的每个补丁以字节对齐结束。
func (r *meshBitReader) align() {
	r.pos = (r.pos + 7) / 8 * 8
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
func decodeMeshTriangles(shading *pdfShading, controls ...*conversionControl) [][3]meshVertex {
	if shading == nil || shading.mesh == nil || shading.colorSpace == nil || len(shading.mesh.data) == 0 {
		return nil
	}
	mesh := shading.mesh
	reader := &meshBitReader{data: mesh.data}
	if len(controls) > 0 {
		reader.control = controls[0]
	}
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
func decodeMeshPoints(shading *pdfShading, controls ...*conversionControl) (points []ofdMeshPoint, verticesPerRow int, ok bool) {
	if shading == nil || shading.mesh == nil || shading.colorSpace == nil || len(shading.mesh.data) == 0 {
		return nil, 0, false
	}
	mesh := shading.mesh
	reader := &meshBitReader{data: mesh.data}
	if len(controls) > 0 {
		reader.control = controls[0]
	}
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
// 与原始网格一致的三角形邻接关系。PDF 的 EdgeFlag=3（复用上一个三角形的
// v0-v1 边）在 OFD 的 GouraudShd 中没有对应标志，改以完整三点输出同一三角形。
func decodeFreeFormPoints(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading) ([]ofdMeshPoint, bool) {
	components := shading.colorSpace.components
	var points []ofdMeshPoint
	var previous [3]ofdMeshPoint
	hasPrevious := false
	for len(points) < maxVectorMeshPoints {
		flag, ok := reader.read(maxInt(mesh.bitsPerFlag, 2))
		if !ok {
			break
		}
		vertex, ok := readMeshVertex(mesh, reader, components, shading, int(flag))
		if !ok {
			break
		}
		if !hasPrevious || flag == 0 {
			second, ok := readMeshVertex(mesh, reader, components, shading, 1)
			if !ok {
				break
			}
			third, ok := readMeshVertex(mesh, reader, components, shading, 2)
			if !ok {
				break
			}
			previous = [3]ofdMeshPoint{
				{x: vertex.x, y: vertex.y, color: vertex.color, flag: 0},
				{x: second.x, y: second.y, color: second.color, flag: 1},
				{x: third.x, y: third.y, color: third.color, flag: 2},
			}
			points = append(points, previous[0], previous[1], previous[2])
			hasPrevious = true
			continue
		}
		point := ofdMeshPoint{x: vertex.x, y: vertex.y, color: vertex.color, flag: int(flag)}
		switch flag {
		case 1:
			previous = [3]ofdMeshPoint{previous[1], previous[2], point}
			points = append(points, point)
		case 2:
			previous = [3]ofdMeshPoint{previous[0], previous[2], point}
			points = append(points, point)
		case 3:
			// PDF 允许复用上一个三角形的 v0-v1 边（EdgeFlag=3），OFD 没有该
			// 标志；按完整 (0,1,2) 三元组重复输出同一个三角形，邻接状态与
			// 直接连线完全一致，后续三角形不受影响。
			triangle := [3]ofdMeshPoint{
				{x: previous[0].x, y: previous[0].y, color: previous[0].color, flag: 0},
				{x: previous[1].x, y: previous[1].y, color: previous[1].color, flag: 1},
				{x: vertex.x, y: vertex.y, color: vertex.color, flag: 2},
			}
			points = append(points, triangle[0], triangle[1], triangle[2])
			previous = triangle
		default:
			points = append(points, point)
		}
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
	if perRow < 2 || perRow > maxLatticeVerticesPerRow {
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
	points, perRow, ok := decodeMeshPoints(shading, p.control)
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
	triangles := decodeMeshTriangles(shading, p.control)
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
	imageData := rasterizeMeshTriangles(converted, minX, minY, maxX, maxY, p.control)
	if imageData == nil {
		return nil
	}
	img := &creator.Image{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY, Data: imageData, Format: "PNG"}
	img.Alpha = ofdAlpha(p.fillOpacity())
	if clips := p.buildClips(minX, minY); clips != nil {
		img.Clips = clips
	}
	return img
}

// meshRasterPixelsPerMM 是网格光栅化的像素密度（约 152 DPI）。
const meshRasterPixelsPerMM = 6.0

// maxMeshRasterPixels 限制网格位图的像素总数。
const maxMeshRasterPixels = 8 << 20

// rasterizeMeshTriangles 用重心坐标对三角形做 Gouraud 光栅化，返回 PNG 数据。
func rasterizeMeshTriangles(triangles []meshTriangleMM, minX, minY, maxX, maxY float64, controls ...*conversionControl) []byte {
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	w := math.Ceil((maxX - minX) * meshRasterPixelsPerMM)
	h := math.Ceil((maxY - minY) * meshRasterPixelsPerMM)
	if math.IsInf(w, 0) || math.IsInf(h, 0) || math.IsNaN(w) || math.IsNaN(h) {
		return nil
	}
	if w*h > maxMeshRasterPixels {
		scale := math.Sqrt(float64(maxMeshRasterPixels) / (w * h))
		w, h = w*scale, h*scale
	}
	width, height := int(w), int(h)
	if width <= 0 || height <= 0 {
		return nil
	}
	if control != nil {
		pixels, err := control.imagePixels(int64(width), int64(height))
		if err != nil || control.addImagePixels(pixels) != nil {
			return nil
		}
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
			if hiX >= loX && control.consumeOperations(int64(hiX-loX)+1) != nil {
				return nil
			}
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
	if err := media.EncodePNG(&encoded, img); err != nil {
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
		} else if flag == 3 {
			// PDF 的 EdgeFlag=3 复用上一个三角形的 v0-v1 边。
			previous = [3]meshVertex{previous[0], previous[1], vertex}
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
	if perRow < 2 || perRow > maxLatticeVerticesPerRow {
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
	// 函数型网格着色（Type 6/7 同样适用）流里编码的是函数输入，读取的分量数
	// 必须取输入维数而不是颜色空间分量数，否则整个补丁错位。
	components := shading.colorSpace.components
	useFunction := len(shading.functions) > 0
	if useFunction {
		components = mesh.streamComponents
		if components <= 0 {
			components = 1
		}
	}
	pointCount := 12
	if mesh.kind == 7 {
		pointCount = 16
	}
	// patchPointIndex 是规范（PDF 32000-1 表 90）的 4×4 张量点阵排列：p0..p3
	// 是左边（u=0），p3..p6 是上边，p6..p9 是右边，p9..p11 是下边，p12..p15
	// 是 4 个内部点。每个元素是把第 i 个流控制点放进 lattice[u][v] 的位置。
	var previous [16][2]float64
	var previousColors [4]pdfColor
	havePrevious := false
	var triangles [][3]meshVertex
	for len(triangles) < maxMeshTriangles {
		rawFlag, ok := reader.read(mesh.bitsPerFlag)
		if !ok {
			break
		}
		flag := int(rawFlag) & 3
		var points [16][2]float64
		var colors [4]pdfColor
		startPoint, startColor := 0, 0
		if flag != 0 {
			// flag 1/2/3：本补丁的左边/上边/右边与上一补丁共享，共享的 4 个
			// 控制点与 2 个角颜色在流中省略，从上一补丁对应边取回。
			// 规范只定义 flag 0..3；没有可共享的上一补丁时无法定位，停止。
			if !havePrevious {
				break
			}
			for i := 0; i < 4; i++ {
				points[i] = previous[(flag*3+i)%12]
			}
			colors[0], colors[1] = previousColors[flag], previousColors[(flag+1)%4]
			startPoint, startColor = 4, 2
		}
		for i := startPoint; i < pointCount; i++ {
			rawX, okX := reader.read(mesh.bitsPerCoordinate)
			rawY, okY := reader.read(mesh.bitsPerCoordinate)
			if !okX || !okY {
				return triangles
			}
			points[i] = [2]float64{mesh.decodeCoordinate(rawX, 0), mesh.decodeCoordinate(rawY, 1)}
		}
		for i := startColor; i < 4; i++ {
			color, ok := readMeshColor(mesh, reader, shading, components)
			if !ok {
				return triangles
			}
			colors[i] = color
		}
		// 每个补丁后位流按字节对齐（规范要求补齐到字节边界）。
		reader.align()
		lattice := patchLattice(points)
		sample := coonsSample
		if mesh.kind == 7 {
			sample = tensorSample
		}
		triangles = append(triangles, tessellateGrid(sample(lattice, colors), patchTessellationSteps)...)
		previous, previousColors, havePrevious = points, colors, true
	}
	return triangles
}

// patchPointIndex 把补丁流中的第 i 个控制点映射到 4×4 张量点阵的 [u][v] 位置。
var patchPointIndex = [16][2]int{
	{0, 0}, {0, 1}, {0, 2}, {0, 3}, {1, 3}, {2, 3}, {3, 3}, {3, 2},
	{3, 1}, {3, 0}, {2, 0}, {1, 0}, {1, 1}, {1, 2}, {2, 2}, {2, 1},
}

// patchLattice 按规范顺序把 16 个流控制点放进 [u][v] 点阵。Type 6 只使用
// 前 12 个边界点，内部 4 格留零，由 Coons 采样补齐；Type 7 使用全部 16 点。
func patchLattice(points [16][2]float64) [4][4][2]float64 {
	var lattice [4][4][2]float64
	for i, index := range patchPointIndex {
		lattice[index[0]][index[1]] = points[i]
	}
	return lattice
}

// patchSample 在单位参数域 [0,1]² 上返回补丁的坐标与颜色。
type patchSample func(u, v float64) (float64, float64, pdfColor)

// tensorSample 对 Type 7 的 16 点张量点阵做双三次 Bézier 求值与角点颜色双线性插值。
// 角点颜色顺序与规范一致：colors[0..3] 对应 (0,0)、(0,1)、(1,1)、(1,0)。
func tensorSample(lattice [4][4][2]float64, colors [4]pdfColor) patchSample {
	return func(u, v float64) (float64, float64, pdfColor) {
		bu := bezierBasis(u)
		bv := bezierBasis(v)
		var x, y float64
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				w := bu[i] * bv[j]
				x += w * lattice[i][j][0]
				y += w * lattice[i][j][1]
			}
		}
		return x, y, patchCornerColor(colors, u, v)
	}
}

// coonsSample 对 Type 6 的 12 点 Coons 曲面求值：四条边界曲线相加后减去
// 双线性混合项。边界为 lattice 的 u=0/u=1 两列与 v=0/v=1 两行。
func coonsSample(lattice [4][4][2]float64, colors [4]pdfColor) patchSample {
	return func(u, v float64) (float64, float64, pdfColor) {
		left := bezierPoint(lattice[0][0], lattice[0][1], lattice[0][2], lattice[0][3], v)
		right := bezierPoint(lattice[3][0], lattice[3][1], lattice[3][2], lattice[3][3], v)
		bottom := bezierPoint(lattice[0][0], lattice[1][0], lattice[2][0], lattice[3][0], u)
		top := bezierPoint(lattice[0][3], lattice[1][3], lattice[2][3], lattice[3][3], u)
		corners := [4][2]float64{lattice[0][0], lattice[3][0], lattice[3][3], lattice[0][3]}
		bilinearity := [2]float64{
			(1-u)*(1-v)*corners[0][0] + u*(1-v)*corners[1][0] + u*v*corners[2][0] + (1-u)*v*corners[3][0],
			(1-u)*(1-v)*corners[0][1] + u*(1-v)*corners[1][1] + u*v*corners[2][1] + (1-u)*v*corners[3][1],
		}
		x := (1-u)*left[0] + u*right[0] + (1-v)*bottom[0] + v*top[0] - bilinearity[0]
		y := (1-u)*left[1] + u*right[1] + (1-v)*bottom[1] + v*top[1] - bilinearity[1]
		return x, y, patchCornerColor(colors, u, v)
	}
}

// patchCornerColor 在四个角点颜色间双线性插值。角点顺序与规范一致：
// colors[0]@(0,0)、colors[1]@(0,1)、colors[2]@(1,1)、colors[3]@(1,0)。
func patchCornerColor(colors [4]pdfColor, u, v float64) pdfColor {
	weights := [4]float64{(1 - u) * (1 - v), (1 - u) * v, u * v, u * (1 - v)}
	red := weights[0]*float64(colors[0].r) + weights[1]*float64(colors[1].r) + weights[2]*float64(colors[2].r) + weights[3]*float64(colors[3].r)
	green := weights[0]*float64(colors[0].g) + weights[1]*float64(colors[1].g) + weights[2]*float64(colors[2].g) + weights[3]*float64(colors[3].g)
	blue := weights[0]*float64(colors[0].b) + weights[1]*float64(colors[1].b) + weights[2]*float64(colors[2].b) + weights[3]*float64(colors[3].b)
	clamp := func(value float64) uint8 {
		if value <= 0 {
			return 0
		}
		if value >= 255 {
			return 255
		}
		return uint8(math.Round(value))
	}
	return pdfColor{r: clamp(red), g: clamp(green), b: clamp(blue)}
}

// readMeshColor 读取一个补丁角点的颜色。函数型网格着色流里编码的是函数输入，
// 颜色由函数求值得到；否则按颜色空间分量解码。
func readMeshColor(mesh *pdfMeshData, reader *meshBitReader, shading *pdfShading, components int) (pdfColor, bool) {
	values := make([]float64, components)
	for c := 0; c < components; c++ {
		raw, ok := reader.read(mesh.bitsPerComponent)
		if !ok {
			return pdfColor{}, false
		}
		values[c] = mesh.decodeComponent(raw, c)
	}
	if len(shading.functions) > 0 {
		return shading.meshColorAt(values)
	}
	return shading.colorSpace.colorFromComponents(values)
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
	// 函数型网格着色：流里编码的是函数输入，颜色由函数求值得到。
	if len(shading.functions) > 0 {
		stored := mesh.streamComponents
		if stored <= 0 {
			stored = 1
		}
		values := make([]float64, stored)
		for c := 0; c < stored; c++ {
			raw, ok := reader.read(mesh.bitsPerComponent)
			if !ok {
				return meshVertex{}, false
			}
			values[c] = mesh.decodeComponent(raw, c)
		}
		color, ok := shading.meshColorAt(values)
		if !ok {
			return meshVertex{}, false
		}
		return meshVertex{x: mesh.decodeCoordinate(rawX, 0), y: mesh.decodeCoordinate(rawY, 1), color: color, flag: flag, usable: true}, true
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
