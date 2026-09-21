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
	if len(block.ImageObject) != 0 {
		t.Fatal("tensor patch mesh should be emitted as vector, not an image")
	}
	var gouraud *models.CTGouraudShd
	var boundary models.StBox
	for index := range block.PathObject {
		color := block.PathObject[index].FillColor
		if color != nil && color.GouraudShd != nil {
			gouraud = color.GouraudShd
			boundary = block.PathObject[index].Boundary
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
	if len(block.ImageObject) != 0 {
		t.Fatal("lattice mesh should be emitted as vector, not an image")
	}
	var laGouraud *models.CTLaGouraudShd
	for index := range block.PathObject {
		color := block.PathObject[index].FillColor
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
