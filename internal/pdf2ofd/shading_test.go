package pdf2ofd

import (
	"bytes"
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
	color := layerPaths(page.Content().Layer[0])[0].FillColor
	if color == nil || color.AxialShd == nil {
		t.Fatalf("fill color = %+v, want axial shading", color)
	}
	// 100pt * 25.4/72 = 35.28mm；若被 CTM 放大则约为 70.56。
	if endX := color.AxialShd.EndPoint.X; endX < 34 || endX > 36.5 {
		t.Fatalf("EndPoint.X = %g, want ~35.28 (unscaled by CTM)", endX)
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
