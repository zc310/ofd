package pdf2ofd

import (
	"archive/zip"
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func TestShadingColorAtSamplesFunction(t *testing.T) {
	// 一维白到黑的指数函数。
	function := &pdfFunction{fnType: 2, c0: []float64{1, 1, 1}, c1: []float64{0, 0, 0}, exp: 1}
	shading := &pdfShading{shadingType: 2, colorSpace: deviceRGBSpace, functions: []*pdfFunction{function}}
	if got, ok := shading.colorAt(0); !ok || got != (pdfColor{r: 255, g: 255, b: 255}) {
		t.Fatalf("t=0 = %+v (ok=%v), want white", got, ok)
	}
	if got, ok := shading.colorAt(1); !ok || got != (pdfColor{r: 0, g: 0, b: 0}) {
		t.Fatalf("t=1 = %+v (ok=%v), want black", got, ok)
	}
}

func TestShadingColorAtUsesComponentFunctions(t *testing.T) {
	// 函数数组：每个分量一个函数。红通道恒 1，绿蓝通道恒 0。
	red := &pdfFunction{fnType: 2, c0: []float64{1}, c1: []float64{1}, exp: 1}
	zero := &pdfFunction{fnType: 2, c0: []float64{0}, c1: []float64{0}, exp: 1}
	shading := &pdfShading{shadingType: 2, colorSpace: deviceRGBSpace, functions: []*pdfFunction{red, zero, zero}}
	if got, ok := shading.colorAt(0.5); !ok || got != (pdfColor{r: 255, g: 0, b: 0}) {
		t.Fatalf("component functions = %+v (ok=%v), want red", got, ok)
	}
}

func TestConvertEmitsAxialShadingForScrollOperator(t *testing.T) {
	// sh 用轴向渐变填充当前裁剪矩形，应输出带 AxialShd 的 PathObject。
	content := []byte("q 0 0 100 100 re W n /Sh1 sh Q")
	pdf := shadingPDF(content, "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	color := paths[0].FillColor
	if color == nil || color.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", color)
	}
	if len(color.AxialShd.Segment) < 2 {
		t.Fatalf("axial segments = %d, want >= 2", len(color.AxialShd.Segment))
	}
	first := color.AxialShd.Segment[0].Color.Value
	if first == nil || first.R != 255 || first.G != 0 || first.B != 0 {
		t.Fatalf("first stop = %+v, want red", first)
	}
}

func TestConvertEmitsRadialShadingForScrollOperator(t *testing.T) {
	// 与 sample2.pdf 第 2 页一致：径向渐变应输出 RadialShd。
	content := []byte("q 0 0 100 100 re W n /Sh1 sh Q")
	pdf := shadingPDF(content, "<< /ShadingType 3 /ColorSpace /DeviceRGB /Coords [50 50 0 50 50 50] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [0 0 0] /N 1 >> /Extend [true true] >>")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	color := paths[0].FillColor
	if color == nil || color.RadialShd == nil {
		t.Fatalf("fill color = %+v, want radial shading", color)
	}
	if color.RadialShd.EndRadius <= 0 {
		t.Fatalf("radial end radius = %g, want > 0", color.RadialShd.EndRadius)
	}
}

func TestConvertShadingCoordinatesAreRelativeToPathBoundary(t *testing.T) {
	// 渐变坐标必须相对路径边界，否则阅读器会再叠加一次边界产生偏移。
	// 裁剪矩形为 (0,0)-(100,100)，页面坐标翻转后边界顶部 Y 非零；着色
	// 起点取矩形顶边 (0,100)，转换后 StartPoint 应落在路径边界原点。
	content := []byte("q 0 0 100 100 re W n /Sh1 sh Q")
	pdf := shadingPDF(content, "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 100 100 100] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	color := layerPaths(page.Content().Layer[0])[0].FillColor
	if color == nil || color.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", color)
	}
	start := color.AxialShd.StartPoint
	if start.X != 0 || start.Y != 0 {
		t.Fatalf("StartPoint = %+v, want (0,0) relative to boundary", start)
	}
}

func TestConvertEmitsShadingForPatternFill(t *testing.T) {
	// canvas 生成的 PDF 用 Pattern 颜色空间 + scn 填充渐变（不是 sh 操作符），
	// 转换后必须保留 AxialShd，否则渐变会退化成纯色。
	content := []byte("/Pattern cs /P1 scn 0 0 100 100 re f")
	pdf := patternShadingPDF(content, "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	color := paths[0].FillColor
	if color == nil || color.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", color)
	}
	first := color.AxialShd.Segment[0].Color.Value
	if first == nil || first.R != 255 || first.G != 0 || first.B != 0 {
		t.Fatalf("first stop = %+v, want red", first)
	}
}

func TestConvertPatternShadingIgnoresContentCTM(t *testing.T) {
	// PatternType 2 图案坐标位于页面默认坐标空间，绘制时不再叠加当前 CTM。
	// 内容以 2 倍缩放绘制 100x100 矩形时，渐变终点仍应落在页面坐标 100（约
	// 35.28mm），而不是被放大到 200。
	content := []byte("q 2 0 0 2 0 0 cm /Pattern cs /P1 scn 0 0 100 100 re f Q")
	pdf := patternShadingPDF(content, "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	color := layerPaths(page.Content().Layer[0])[0].FillColor
	if color == nil || color.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", color)
	}
	// 100pt * 25.4/72 = 35.28mm；若被 CTM 放大则约为 70.56。
	if endX := color.AxialShd.EndPoint.X; endX < 34 || endX > 36.5 {
		t.Fatalf("EndPoint.X = %g, want ~35.28 (unscaled by CTM)", endX)
	}
}

// gradientTextPDF 构造单页 PDF，文字使用 PatternType 2 图案 /P1 作为填充，
// 资源同时提供 /F1 字体。
func gradientTextPDF(content []byte, shading string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 7 0 R >> /Pattern << /P1 6 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		shading,
		"<< /Type /Pattern /PatternType 2 /Matrix [1 0 0 1 0 0] /Shading 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	return assemblePDF(objects)
}

func TestConvertEmitsShadingForGradientTextFill(t *testing.T) {
	// 文字用 Pattern 颜色空间渐变填充（电子发票的彩色标题）时，必须把着色转成
	// 文字对象边界的局部渐变；否则文字退化成纯色/黑色，字体上的渐变丢失。
	content := []byte("/Pattern cs /P1 scn BT /F1 12 Tf 20 100 Td (Hi) Tj ET")
	pdf := gradientTextPDF(content, "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>")
	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) != 1 {
		t.Fatalf("text objects = %d, want 1", len(texts))
	}
	color := texts[0].FillColor
	if color == nil || color.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", color)
	}
	first := color.AxialShd.Segment[0].Color.Value
	if first == nil || first.R != 255 || first.G != 0 || first.B != 0 {
		t.Fatalf("first stop = %+v, want red", first)
	}
	// 渐变坐标必须相对文字边界：着色终点页面 X=100pt≈35.28mm，减去文字
	// 边界 X=20pt≈7.06mm 后约为 28.22mm；若未减边界会得到 35.28。
	if endX := color.AxialShd.EndPoint.X; endX < 27.9 || endX > 28.5 {
		t.Fatalf("EndPoint.X = %g, want ~28.22 relative to text boundary", endX)
	}
}

// patternShadingPDF 构造单页 PDF，资源中带 PatternType 2 图案 /P1。
func patternShadingPDF(content []byte, shading string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Pattern << /P1 6 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		shading,
		"<< /Type /Pattern /PatternType 2 /Matrix [1 0 0 1 0 0] /Shading 5 0 R >>",
	}
	return assemblePDF(objects)
}

// shadingPDF 构造单页 PDF，资源中带 /Sh1 着色定义。
func shadingPDF(content []byte, shading string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Shading << /Sh1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		shading,
	}
	return assemblePDF(objects)
}

func TestConvertShadingAppliesExtGStateFillAlpha(t *testing.T) {
	// sh 绘制的着色同样受当前非描边透明度 ca 影响（alphatrans.pdf：/GS1 gs
	// 设 ca=0.5 后 /Sh1 sh）。忽略它会把半透明渐变渲染成不透明色块。
	content := "q 0 0 100 100 re W n /GS1 gs /Sh1 sh Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ExtGState << /GS1 5 0 R >> /Shading << /Sh1 6 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /ExtGState /ca 0.5 >>",
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>",
	}
	page := parseConvertedPage(t, objects)
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	if paths[0].FillColor == nil || paths[0].FillColor.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", paths[0].FillColor)
	}
	alpha := paths[0].Alpha
	if alpha == nil {
		t.Fatal("着色路径 Alpha 为 nil，ca=0.5 未生效")
	}
	if *alpha < 126 || *alpha > 130 {
		t.Fatalf("着色路径 Alpha = %d, want ~128", *alpha)
	}
}

// writeMeshBits 按位打包一个 Type 7 网格补丁流。
type writeMeshBits struct {
	data    []byte
	current uint32
	filled  int
}

func (w *writeMeshBits) put(value uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		w.current = w.current<<1 | ((value >> uint(i)) & 1)
		w.filled++
		if w.filled == 8 {
			w.data = append(w.data, byte(w.current))
			w.current, w.filled = 0, 0
		}
	}
}

func (w *writeMeshBits) done() []byte {
	if w.filled > 0 {
		w.data = append(w.data, byte(w.current<<uint(8-w.filled)))
		w.current, w.filled = 0, 0
	}
	return w.data
}

// freeFormTriangle 写出一个 Type 4 自由三角网格：边标志 + 三个顶点。
// 坐标 12 位、函数输入 8 位。
func freeFormTriangle(w *writeMeshBits, vertices [3][3]int) {
	w.put(0, 2) // BitsPerFlag=2，flag=0 表示全新三角形
	for _, vertex := range vertices {
		w.put(uint32(vertex[0]), 12)
		w.put(uint32(vertex[1]), 12)
		w.put(uint32(vertex[2]), 8)
	}
}

func TestConvertFillsPathWithMeshShadingPattern(t *testing.T) {
	// PatternType 2（着色图案）经 scn 选中后作为路径填充色。网格着色此前在
	// shadingColor 里直接返回 nil，路径保留 defaultW 的黑色填充，整页渐变
	// 背景会变成一块黑板（tensor-allflags-withfunction.pdf）。
	//
	// 颜色由 /Function 从网格输入求值（1 入 1 出，映射到红通道）。
	// Decode 把 12 位原始值线性映射到 0..100。网格只覆盖页面的左半部分，
	// Gouraud 的边缘延展会把颜色铺满整条路径，网格外实际上并没有着色，
	// 因此填充路径必须收缩到网格包围盒。
	const half = 2048
	var w writeMeshBits
	freeFormTriangle(&w, [3][3]int{{0, 0, 0}, {half, 0, 128}, {0, 4095, 255}})
	freeFormTriangle(&w, [3][3]int{{half, 0, 128}, {half, 4095, 200}, {0, 4095, 255}})
	stream := w.done()

	meshDict := "<< /ShadingType 4 /ColorSpace /DeviceRGB " +
		"/Decode [0 100 0 100 0 1] /Function 7 0 R " +
		"/BitsPerCoordinate 12 /BitsPerComponent 8 /BitsPerFlag 2 " +
		"/Length " + itoa(len(stream)) + " >>stream\n" + string(stream) + "\nendstream"
	content := []byte("/Pattern cs /P1 scn 0 0 100 100 re f")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Pattern << /P1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /Pattern /PatternType 2 /Shading 6 0 R >>",
		meshDict,
		// 1 入 1 出：把网格输入值映射到红通道，绿蓝固定为 0。
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
		t.Fatal(err)
	}
	pageXML := extractPageContent(t, output.Bytes())
	// 此前这里只剩一条纯黑 FillColor 的整页矩形，渐变完全丢失。
	if !strings.Contains(pageXML, "<ofd:GouraudShd") && !strings.Contains(pageXML, "<ofd:LaGouraudShd") {
		t.Fatalf("mesh shading pattern fell back to a solid fill:\n%s", pageXML)
	}
	// 颜色必须由 /Function 求值：输入 128/255 映射到红通道 128/255。
	if !strings.Contains(pageXML, `<ofd:Color Value="128 0 0"/>`) {
		t.Fatalf("mesh colour was not evaluated through /Function:\n%s", pageXML)
	}
	// 填充路径必须收缩到网格包围盒（约半页宽 50mm），否则 Gouraud 的边缘延展
	// 会把整条 100x100 的路径涂满。
	const boundaryPrefix = `<ofd:PathObject ID="3" Boundary="0 0 `
	index := strings.Index(pageXML, boundaryPrefix)
	if index < 0 {
		t.Fatalf("path object boundary not found:\n%s", pageXML)
	}
	rest := pageXML[index+len(boundaryPrefix):]
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		t.Fatalf("path boundary is malformed:\n%s", pageXML)
	}
	width, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		t.Fatalf("path width %q is not a number:\n%s", fields[0], pageXML)
	}
	if width > 55 {
		t.Fatalf("path width = %.2fmm, want ~50mm (the mesh bounds), not the full 100mm", width)
	}
}

// extractPageContent 解出转换结果第一页的 Content.xml。
func extractPageContent(t *testing.T, ofd []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(ofd), int64(len(ofd)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != "Doc_0/Pages/Page_0/Content.xml" {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer handle.Close()
		data, err := io.ReadAll(handle)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatal("page content not found in OFD")
	return ""
}
