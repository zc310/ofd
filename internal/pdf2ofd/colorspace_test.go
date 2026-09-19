package pdf2ofd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
)

// assemblePDF 把对象正文组装成带 xref 的最小 PDF。
func assemblePDF(objects []string) []byte {
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}

func itoa(value int) string { return fmt.Sprintf("%d", value) }

func TestSeparationColorSpaceAppliesTintTransform(t *testing.T) {
	// 与 sample2.pdf 第 3 页一致：/Separation /All 的 tint 0 是白色。
	// 忽略颜色空间会把 tint 0 当作灰度黑，整页背景变黑。
	tint := &pdfFunction{fnType: 2, c0: []float64{1, 1, 1}, c1: []float64{0, 0, 0}, exp: 1}
	space := &pdfColorSpace{family: "Separation", components: 1, base: deviceRGBSpace, tint: tint}

	if got, ok := space.colorFromComponents([]float64{0}); !ok || got != (pdfColor{r: 255, g: 255, b: 255}) {
		t.Fatalf("tint 0 = %+v (ok=%v), want white", got, ok)
	}
	if got, ok := space.colorFromComponents([]float64{1}); !ok || got != (pdfColor{r: 0, g: 0, b: 0}) {
		t.Fatalf("tint 1 = %+v (ok=%v), want black", got, ok)
	}
}

func TestIndexedColorSpaceLooksUpPalette(t *testing.T) {
	// 调色板：索引 0 红、1 绿、2 蓝。
	palette := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255}
	space := &pdfColorSpace{family: "Indexed", components: 1, base: deviceRGBSpace, lookup: palette, high: 2}
	if got, ok := space.colorFromComponents([]float64{1}); !ok || got != (pdfColor{r: 0, g: 255, b: 0}) {
		t.Fatalf("index 1 = %+v (ok=%v), want green", got, ok)
	}
	if _, ok := space.colorFromComponents([]float64{3}); ok {
		t.Fatal("index above High should fail")
	}
}

func TestDeviceNColorSpaceUsesTintComponents(t *testing.T) {
	// DeviceN 有两个分量，tint 变换把第一分量映射到灰度。
	tint := &pdfFunction{fnType: 2, c0: []float64{0}, c1: []float64{1}, exp: 1}
	space := &pdfColorSpace{family: "DeviceN", components: 2, base: deviceGraySpace, tint: tint}
	if got, ok := space.colorFromComponents([]float64{0.5, 0.9}); !ok || got != (pdfColor{r: 127, g: 127, b: 127}) {
		t.Fatalf("DeviceN 0.5 = %+v (ok=%v), want 50%% gray", got, ok)
	}
	if _, ok := space.colorFromComponents([]float64{0.5}); ok {
		t.Fatal("too few components should fail")
	}
}

func TestSampledFunctionInterpolates(t *testing.T) {
	// 一维采样函数，2 个采样点：0 -> (0,0,0)，1 -> (1,1,1)。
	fn := &pdfFunction{
		fnType:  0,
		domain:  []float64{0, 1},
		rng:     []float64{0, 1, 0, 1, 0, 1},
		size:    []int{2},
		bits:    8,
		samples: []byte{0, 0, 0, 255, 255, 255},
	}
	got, ok := fn.eval([]float64{0.5})
	if !ok {
		t.Fatal("eval failed")
	}
	for i, value := range got {
		if value < 0.49 || value > 0.51 {
			t.Fatalf("component %d = %g, want about 0.5", i, value)
		}
	}
}

func TestStitchingFunctionSelectsSubFunction(t *testing.T) {
	// Bounds [0.5]，两个子函数：下段恒 0，上段恒 1。
	lower := &pdfFunction{fnType: 2, c0: []float64{0}, c1: []float64{0}, exp: 1}
	upper := &pdfFunction{fnType: 2, c0: []float64{1}, c1: []float64{1}, exp: 1}
	fn := &pdfFunction{fnType: 3, domain: []float64{0, 1}, rng: []float64{0, 1}, bounds: []float64{0.5}, enc3: []float64{0, 1, 0, 1}, funcs: []*pdfFunction{lower, upper}}
	if got, ok := fn.eval([]float64{0.25}); !ok || got[0] != 0 {
		t.Fatalf("lower segment = %v (ok=%v), want 0", got, ok)
	}
	if got, ok := fn.eval([]float64{0.75}); !ok || got[0] != 1 {
		t.Fatalf("upper segment = %v (ok=%v), want 1", got, ok)
	}
}

func TestInterpreterAppliesSeparationFillColor(t *testing.T) {
	// 完整链路：cs 选择 Separation，scn tint 0 应得到白色填充。
	interpreter := &pdfInterpreter{
		ctx:   testPDFContext(t),
		fonts: map[string]pdfFontInfo{},
		state: pdfGraphicsState{ctm: identityPDFMatrix(), hScale: 100},
	}
	resources := testSeparationResources(t)
	if err := interpreter.operator("cs", []any{pdfName("Cs8")}, resources, 0); err != nil {
		t.Fatal(err)
	}
	if err := interpreter.operator("scn", []any{float64(0)}, resources, 0); err != nil {
		t.Fatal(err)
	}
	if interpreter.state.fill != (pdfColor{r: 255, g: 255, b: 255}) {
		t.Fatalf("scn fill = %+v, want white", interpreter.state.fill)
	}
}

// TestConvertSeparationPageBackgroundStaysWhite 端到端验证 sample2.pdf 第 3 页
// 的 Separation 白色背景不会再被渲染成整页黑色。
func TestConvertSeparationPageBackgroundStaysWhite(t *testing.T) {
	content := []byte("q /Cs8 cs 0 scn 0 0 226.95 301.99 re f Q 0 0 0 rg 10 10 20 20 re f")
	pdf := separationPDF(content)
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) != 2 {
		t.Fatalf("path objects = %d, want 2", len(paths))
	}
	color := paths[0].FillColor
	if color == nil || color.Value == nil || color.Value.R != 255 || color.Value.G != 255 || color.Value.B != 255 {
		t.Fatalf("background fill = %+v, want white", color)
	}
}

// testSeparationResources 构造带 /Separation /All tint 变换的颜色空间资源，
// tint 0 映射为白色、tint 1 映射为黑色。
func testSeparationResources(t *testing.T) types.Dict {
	t.Helper()
	// FunctionType 2 指数插值函数：C0=[1 1 1]，C1=[0 0 0]，N=1。
	function := types.Dict{
		"FunctionType": types.Integer(2),
		"Domain":       types.Array{types.Integer(0), types.Integer(1)},
		"C0":           types.Array{types.Integer(1), types.Integer(1), types.Integer(1)},
		"C1":           types.Array{types.Integer(0), types.Integer(0), types.Integer(0)},
		"N":            types.Integer(1),
	}
	separation := types.Array{
		types.Name("Separation"),
		types.Name("All"),
		types.Name("DeviceRGB"),
		function,
	}
	return types.Dict{
		"ColorSpace": types.Dict{"Cs8": separation},
	}
}

// separationPDF 构造单页 PDF，资源包含 /Cs8 的 Separation 颜色空间。
func separationPDF(content []byte) []byte {
	resources := "<< /ColorSpace << /Cs8 [/Separation /All /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [0 0 0] /N 1 >>] >> /Font << /F1 6 0 R >> >>"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources " + resources + " /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	return assemblePDF(objects)
}

// testPDFContext 返回一个不依赖真实文件的最小 pdfcpu 上下文。
func testPDFContext(t *testing.T) *model.Context {
	t.Helper()
	pdf := separationPDF([]byte("q Q"))
	ctx, err, _ := readPDFContext(pdf, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
