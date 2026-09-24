package pdf2ofd

import (
	"bytes"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestConvertTensorPatchMeshEmitsGouraudPath(t *testing.T) {
	// Adobe 生成的 Type 7（Tensor）网格按 Coons 边界顺序排列控制点，
	// 角点为 p0/p3/p6/p9。转换后应输出覆盖网格范围的矢量 Gouraud 网格，
	// 而不是位图。
	mesh := buildAdobeTensorPatchStream([4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}})
	dict := "/ShadingType 7 /ColorSpace /DeviceRGB /BitsPerCoordinate 32 /BitsPerComponent 8 /BitsPerFlag 8" +
		" /Decode [0 100 0 100 0 1 0 1 0 1] /Length " + itoa(len(mesh))
	shading := "<< " + dict + " >>\nstream\n" + string(mesh) + "\nendstream"
	content := []byte("q /Sh0 sh Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Shading << /Sh0 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		shading,
	}
	pdf := assemblePDF(objects)
	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	block := page.Content().Layer[0]
	if len(layerImages(block)) != 0 {
		t.Fatal("tensor patch mesh should be emitted as vector, not an image")
	}
	var gouraud *models.CTGouraudShd
	var boundary models.StBox
	for index := range layerPaths(block) {
		color := layerPaths(block)[index].FillColor
		if color != nil && color.GouraudShd != nil {
			gouraud = color.GouraudShd
			boundary = layerPaths(block)[index].Boundary
			break
		}
	}
	if gouraud == nil {
		t.Fatal("tensor patch mesh produced no Gouraud shading")
	}
	// 网格覆盖整页（100pt ≈ 35.28mm），填充路径边界应接近该值。
	if boundary.Width < 30 || boundary.Width > 36 {
		t.Fatalf("mesh path width = %g, want ~35.28", boundary.Width)
	}
	if len(gouraud.Point) < 3 {
		t.Fatalf("gouraud points = %d, want >= 3", len(gouraud.Point))
	}
	for _, point := range gouraud.Point {
		if point.Color.Value == nil {
			t.Fatal("gouraud point has no color value")
		}
		if point.Color.Value.R < 150 || point.Color.Value.G > 80 || point.Color.Value.B > 80 {
			t.Fatalf("gouraud point color = %+v, want red", point.Color.Value.RGBA)
		}
	}
}

// TestDecodeFreeFormPointsEdgeFlag3ReusesFirstEdge 回归：PDF 自由网格的
// EdgeFlag=3（复用上一个三角形的 v0-v1 边）此前被 decodeFreeFormPoints
// 当作普通单点丢弃/错误连线；现在应按完整 (0,1,2) 三元组输出同一个三角形，
// 并且后续邻接链（flag 1）仍与 flag 3 输出的三角形正确衔接。
func TestDecodeFreeFormPointsEdgeFlag3ReusesFirstEdge(t *testing.T) {
	writer := &meshBitWriter{}
	maxCoord := float64(math.MaxUint32)
	writeCoords := func(x, y, r, g, b float64) {
		writer.write(uint32(x*maxCoord), 32)
		writer.write(uint32(y*maxCoord), 32)
		writer.write(uint32(r), 8)
		writer.write(uint32(g), 8)
		writer.write(uint32(b), 8)
	}
	// 位流：flag0 后紧跟三个顶点（三角形起始点之后不为 v1/v2 再写 flag），
	// 之后每个顶点 = flag(2 bits) + 坐标 + 颜色。
	writer.write(0, 2)
	writeCoords(0.02, 0.02, 200, 10, 10) // v0 = (2,2)
	writeCoords(0.18, 0.02, 10, 200, 10) // v1 = (18,2)
	writeCoords(0.10, 0.18, 10, 10, 200) // v2 = (10,18)
	writer.write(3, 2)
	writeCoords(0.10, 0.32, 200, 200, 10) // v3 = (10,32)，flag 3 → {v0,v1,v3}
	writer.write(1, 2)
	writeCoords(0.18, 0.52, 10, 10, 200) // v4 = (18,52)，flag 1 → 衔接 {v1,v3,v4}

	shading := &pdfShading{
		shadingType: 4,
		colorSpace:  deviceRGBSpace,
		mesh: &pdfMeshData{
			kind:              4,
			bitsPerCoordinate: 32,
			bitsPerComponent:  8,
			bitsPerFlag:       2,
			decode:            []float64{0, 100, 0, 100, 0, 1, 0, 1, 0, 1},
			data:              writer.bytes(),
		},
	}
	points, ok := decodeFreeFormPoints(shading.mesh, &meshBitReader{data: shading.mesh.data}, shading)
	if !ok {
		t.Fatal("decodeFreeFormPoints failed")
	}
	wantXY := [][2]float64{
		{2, 2}, {18, 2}, {10, 18}, // 三角形 1
		{2, 2}, {18, 2}, {10, 32}, // 三角形 2（flag 3 展开）
		{18, 52}, // flag 1 连接后续
	}
	wantFlag := []int{0, 1, 2, 0, 1, 2, 1}
	wantColor := []pdfColor{
		{200, 10, 10}, {10, 200, 10}, {10, 10, 200},
		{200, 10, 10}, {10, 200, 10}, {200, 200, 10}, {10, 10, 200},
	}
	if len(points) != len(wantXY) {
		t.Fatalf("points = %d, want %d", len(points), len(wantXY))
	}
	for i, point := range points {
		if math.Abs(point.x-wantXY[i][0]) > 1e-6 || math.Abs(point.y-wantXY[i][1]) > 1e-6 {
			t.Fatalf("point %d pos = (%g,%g), want (%g,%g)", i, point.x, point.y, wantXY[i][0], wantXY[i][1])
		}
		if point.flag != wantFlag[i] {
			t.Fatalf("point %d flag = %d, want %d", i, point.flag, wantFlag[i])
		}
		close := func(a, b uint8) bool { d := int(a) - int(b); return d >= -2 && d <= 2 }
		if !close(point.color.r, wantColor[i].r) || !close(point.color.g, wantColor[i].g) || !close(point.color.b, wantColor[i].b) {
			t.Fatalf("point %d color = %+v, want %+v", i, point.color, wantColor[i])
		}
	}
}

// TestDecodeFreeFormMeshEdgeFlag3ReusesFirstEdge 回归：位图回退路径
// decodeFreeFormMesh 对 EdgeFlag=3 同样应复用上一个三角形的 v0-v1 边，
// 否则 meshImage 会丢失三角形。
func TestDecodeFreeFormMeshEdgeFlag3ReusesFirstEdge(t *testing.T) {
	writer := &meshBitWriter{}
	maxCoord := float64(math.MaxUint32)
	writeCoords := func(x, y, r, g, b float64) {
		writer.write(uint32(x*maxCoord), 32)
		writer.write(uint32(y*maxCoord), 32)
		writer.write(uint32(r), 8)
		writer.write(uint32(g), 8)
		writer.write(uint32(b), 8)
	}
	writer.write(0, 2)
	writeCoords(0.02, 0.02, 200, 10, 10)
	writeCoords(0.18, 0.02, 10, 200, 10)
	writeCoords(0.10, 0.18, 10, 10, 200)
	writer.write(3, 2)
	writeCoords(0.10, 0.32, 200, 200, 10)

	shading := &pdfShading{
		shadingType: 4,
		colorSpace:  deviceRGBSpace,
		mesh: &pdfMeshData{
			kind:              4,
			bitsPerCoordinate: 32,
			bitsPerComponent:  8,
			bitsPerFlag:       2,
			decode:            []float64{0, 100, 0, 100, 0, 1, 0, 1, 0, 1},
			data:              writer.bytes(),
		},
	}
	triangles := decodeFreeFormMesh(shading.mesh, &meshBitReader{data: shading.mesh.data}, shading)
	if len(triangles) != 2 {
		t.Fatalf("triangles = %d, want 2", len(triangles))
	}
	// 三角形 2 应为 {v0, v1, v3} = {(2,2),(18,2),(10,32)}。
	second := triangles[1]
	want := [][2]float64{{2, 2}, {18, 2}, {10, 32}}
	for i := 0; i < 3; i++ {
		if math.Abs(second[i].x-want[i][0]) > 1e-6 || math.Abs(second[i].y-want[i][1]) > 1e-6 {
			t.Fatalf("tri2 vertex %d = (%g,%g), want (%g,%g)", i, second[i].x, second[i].y, want[i][0], want[i][1])
		}
	}
}

func TestConvertLatticeMeshEmitsLaGouraudPath(t *testing.T) {
	// Type 5 规则网格应输出 LaGouraudShd，保留每行顶点数。
	mesh := buildLatticeMeshStream(2, [][3]uint8{{200, 10, 10}, {10, 200, 10}, {10, 10, 200}, {200, 200, 10}})
	dict := "/ShadingType 5 /ColorSpace /DeviceRGB /BitsPerCoordinate 32 /BitsPerComponent 8" +
		" /VerticesPerRow 2 /Decode [0 100 0 100 0 1 0 1 0 1] /Length " + itoa(len(mesh))
	shading := "<< " + dict + " >>\nstream\n" + string(mesh) + "\nendstream"
	content := []byte("q /Sh0 sh Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Shading << /Sh0 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		shading,
	}
	pdf := assemblePDF(objects)
	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	block := page.Content().Layer[0]
	if len(layerImages(block)) != 0 {
		t.Fatal("lattice mesh should be emitted as vector, not an image")
	}
	var laGouraud *models.CTLaGouraudShd
	for index := range layerPaths(block) {
		color := layerPaths(block)[index].FillColor
		if color == nil {
			continue
		}
		// 规范 XML Schema 使用 LaGourandShd 拼写，正文使用 LaGouraudShd。
		if color.LaGouraudShd != nil {
			laGouraud = color.LaGouraudShd
			break
		}
		if color.LaGourandShd != nil {
			laGouraud = color.LaGourandShd
			break
		}
	}
	if laGouraud == nil {
		t.Fatal("lattice mesh produced no LaGouraud shading")
	}
	if laGouraud.VerticesPerRow != 2 {
		t.Fatalf("VerticesPerRow = %d, want 2", laGouraud.VerticesPerRow)
	}
	if len(laGouraud.Point) != 4 {
		t.Fatalf("laGouraud points = %d, want 4", len(laGouraud.Point))
	}
}

// buildAdobeTensorPatchStream 按 Adobe 顺序构造单个 Type 7 网格补丁的位流：
// 12 个边界控制点（Coons 顺序）+ 4 个内部点 + 4 个角点颜色。
func buildAdobeTensorPatchStream(corners [4][2]float64) []byte {
	// corners 顺序为 p0、p3、p6、p9。
	p0, p3, p6, p9 := corners[0], corners[1], corners[2], corners[3]
	lerp := func(a, b [2]float64, t float64) [2]float64 {
		return [2]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t}
	}
	points := [][2]float64{
		p0, lerp(p0, p3, 1.0/3), lerp(p0, p3, 2.0/3), p3,
		lerp(p3, p6, 1.0/3), lerp(p3, p6, 2.0/3), p6,
		lerp(p6, p9, 1.0/3), lerp(p6, p9, 2.0/3), p9,
		lerp(p9, p0, 1.0/3), lerp(p9, p0, 2.0/3),
		{0.5, 0.5}, {0.5, 0.5}, {0.5, 0.5}, {0.5, 0.5},
	}
	color := [3]uint8{200, 10, 10}
	writer := &meshBitWriter{}
	writer.write(0, 8)
	max := float64(math.MaxUint32)
	for _, point := range points {
		writer.write(uint32(point[0]*max), 32)
		writer.write(uint32(point[1]*max), 32)
	}
	for i := 0; i < 4; i++ {
		for _, component := range color {
			writer.write(uint32(component), 8)
		}
	}
	return writer.bytes()
}

// buildLatticeMeshStream 按行优先构造 Type 5 规则网格的位流（无 EdgeFlag）。
func buildLatticeMeshStream(verticesPerRow int, colors [][3]uint8) []byte {
	writer := &meshBitWriter{}
	max := float64(math.MaxUint32)
	rows := len(colors) / verticesPerRow
	for row := 0; row < rows; row++ {
		for column := 0; column < verticesPerRow; column++ {
			index := row*verticesPerRow + column
			x := float64(column) / float64(verticesPerRow-1)
			y := float64(row) / float64(rows-1)
			writer.write(uint32(x*max), 32)
			writer.write(uint32(y*max), 32)
			for _, component := range colors[index] {
				writer.write(uint32(component), 8)
			}
		}
	}
	return writer.bytes()
}

type meshBitWriter struct {
	data []byte
	pos  int
}

func (w *meshBitWriter) write(value uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if w.pos%8 == 0 {
			w.data = append(w.data, 0)
		}
		if (value>>uint(i))&1 == 1 {
			w.data[len(w.data)-1] |= 1 << uint(7-w.pos%8)
		}
		w.pos++
	}
}

func (w *meshBitWriter) bytes() []byte { return w.data }
