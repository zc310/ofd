package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
)

func testCalculator(t *testing.T, code string, domain, rng []float64, options Options) *pdfFunction {
	t.Helper()
	options, err := options.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	p := pdfInterpreter{control: &conversionControl{ctx: t.Context(), options: options}}
	array := func(values []float64) types.Array {
		result := make(types.Array, len(values))
		for i, v := range values {
			result[i] = types.Float(v)
		}
		return result
	}
	fn := p.parseFunctionStream(&types.StreamDict{Dict: types.Dict{"FunctionType": types.Integer(4), "Domain": array(domain), "Range": array(rng)}, Content: []byte(code)}, 0)
	if fn == nil {
		t.Fatalf("parse %q failed: %v", code, p.control.check())
	}
	return fn
}

func TestPDFType4CalculatorOperators(t *testing.T) {
	// 整体程序覆盖整数/实数区别、负数舍入、度数三角函数、逻辑右移及栈顺序。
	for _, tc := range []struct {
		code string
		want float64
	}{
		{"-5 abs", 5}, {"3 4 add", 7}, {"1 -1 atan", 135}, {"2.1 ceiling", 3},
		{"60 cos", 0.5}, {"-2.9 cvi", -2}, {"3 cvr 2 div", 1.5}, {"7 2 div", 3.5},
		{"2 3 exp", 8}, {"-2.1 floor", -3}, {"7.9 2.1 idiv", 3}, {"-7 2 idiv", -3},
		{"1 ln", 0}, {"100 log", 2}, {"-7 3 mod", -1}, {"3 4 mul", 12},
		{"-5 neg", 5}, {"-1.5 round", -1}, {"1.5 round", 2}, {"450 sin", 1},
		{"9 sqrt", 3}, {"3 5 sub", -2}, {"-2.9 truncate", -2},
		{"7 3 and", 3}, {"4 1 or", 5}, {"7 3 xor", 4}, {"0 not", -1},
		{"1 3 bitshift", 8}, {"-1 -1 bitshift", 2147483647}, {"1 32 bitshift", 0}, {"-1 -32 bitshift", 0},
		{"true false and { 1 } { 2 } ifelse", 2}, {"true false or { 1 } { 2 } ifelse", 1},
		{"true true xor { 1 } { 2 } ifelse", 2}, {"false not { 9 } if", 9},
		{"1 1.0 eq { 1 } { 0 } ifelse", 1}, {"1 true ne { 1 } { 0 } ifelse", 1},
		{"2 2 ge { 1 } { 0 } ifelse", 1}, {"2 1 gt { 1 } { 0 } ifelse", 1},
		{"2 2 le { 1 } { 0 } ifelse", 1}, {"1 2 lt { 1 } { 0 } ifelse", 1},
		{"false { 1 0 div } if 8", 8}, {"true { false { 1 } { 2 } ifelse } { 3 } ifelse", 2},
		{"1 2 2 copy add add add", 6}, {"3 dup mul", 9}, {"1 2 exch sub", 1},
		{"1 2 1 index add add", 4}, {"1 2 pop", 1}, {"1 2 3 3 1 roll sub sub", 4},
		{"1 2 3 3 -1 roll sub sub", 0}, {"1 0 2147483647 roll", 1},
		{"2147483647 1 add cvr", 2147483648}, {"-2147483648 abs", 2147483648},
		{"% comment\r\n .5 2 mul % comment\n", 1},
	} {
		t.Run(tc.code, func(t *testing.T) {
			fn := testCalculator(t, "{ pop "+tc.code+" }", []float64{0, 1}, []float64{-1e10, 1e10}, Options{})
			got, ok := fn.eval([]float64{0})
			if !ok || len(got) != 1 || math.Abs(got[0]-tc.want) > 1e-8 {
				t.Fatalf("got=%v ok=%v want=%g", got, ok, tc.want)
			}
		})
	}
}

func TestPDFType4ClampsDomainRangeAndPreservesOutputOrder(t *testing.T) {
	fn := testCalculator(t, "{ exch 2 mul exch 3 mul }", []float64{0, 1, 0, 2}, []float64{0, 1, 0, 5}, Options{})
	got, ok := fn.eval([]float64{3, 4})
	if !ok || len(got) != 2 || got[0] != 1 || got[1] != 5 {
		t.Fatalf("got=%v ok=%v", got, ok)
	}
	// Type 3 拼接函数同样可调用 Type 4 子函数。
	stitch := &pdfFunction{fnType: 3, domain: []float64{0, 1}, enc3: []float64{0, 1}, funcs: []*pdfFunction{testCalculator(t, "{ 2 mul }", []float64{0, 1}, []float64{0, 1}, Options{})}}
	got, ok = stitch.eval([]float64{0.25})
	if !ok || got[0] != 0.5 {
		t.Fatalf("stitched=%v ok=%v", got, ok)
	}
}

func TestPDFType4RejectsInvalidProgramsAndValues(t *testing.T) {
	for _, code := range []string{"1", "{ 1", "{1} 2", "{ repeat }", "{ /name }", "{ NaN }", "{ 0x1p0 }", "{ 1_000 }", "{ 1e999 }", "{ [1] }"} {
		if _, err := compilePDFCalculator([]byte(code), []float64{0, 1}, []float64{0, 1}, nil); err == nil {
			t.Fatalf("accepted %q", code)
		}
	}
	for _, code := range []string{"pop pop", "true 1 add", "1 0 div", "-1 sqrt", "0 ln", "0 0 atan", "1 -1 copy", "1 2 index", "1 2 1 roll", "1.0 1 bitshift", "1.0 not", "1 true and", "true", "{1}", "1 {2} if", "2147483648 cvi", "-2147483648 -1 idiv", "1.0 2 mod"} {
		var diagnostics []Diagnostic
		fn := testCalculator(t, "{ pop "+code+" }", []float64{0, 1}, []float64{-1e10, 1e10}, Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if got, ok := fn.eval([]float64{0}); ok {
			t.Fatalf("accepted %q: %v", code, got)
		}
		if !errors.Is(fn.calculator.control.check(), ErrUnsupported) || len(diagnostics) != 1 || diagnostics[0].Code != "function.type4" {
			t.Fatalf("%q diagnostics=%v error=%v", code, diagnostics, fn.calculator.control.check())
		}
	}
}

func calculatorPDF(code string, shading bool) []byte {
	content := "/Spot cs 0.25 scn 20 20 60 60 re f /Spot cs 0.75 scn 120 20 60 60 re f BT /F1 12 Tf 20 110 Td (Type4 colours) Tj ET"
	resources := "/ColorSpace << /Spot [/Separation /Spot /DeviceRGB 5 0 R] >> /Font << /F1 6 0 R >>"
	if shading {
		content = "q 20 20 160 60 re W n /Sh sh Q BT /F1 12 Tf 20 110 Td (Type4 gradient) Tj ET"
		resources = "/Shading << /Sh << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [20 20 180 20] /Function 5 0 R /Extend [true true] >> >> /Font << /F1 6 0 R >>"
	}
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << " + resources + " >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1] /Range [0 1 0 1 0 1] /Length %d >>\nstream\n%s\nendstream", len(code), code),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

func TestPDFType4StrictConversionRendersTintAndGradient(t *testing.T) {
	for _, shading := range []bool{false, true} {
		var output bytes.Buffer
		// r=x, g=0, b=1-x：验证多输出顺序并保留渐变与文字。
		if err := ConvertWithOptions(t.Context(), calculatorPDF("{ dup 0 exch 1 exch sub }", shading), &output, "", Options{Strict: true}); err != nil {
			t.Fatal(err)
		}
		ofd, err := parser.NewOFD(output.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ofd.Close() }()
		page := ofd.Documents[0].Pages[0]
		if err := page.EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
		paths := layerPaths(page.Content().Layer[0])
		if len(paths) == 0 {
			t.Fatal("missing Type4 paths")
		}
		if shading && paths[0].FillColor.AxialShd == nil {
			t.Fatal("Type4 gradient became solid fill")
		}
		if len(layerTexts(page.Content().Layer[0])) != 1 {
			t.Fatal("Type4 conversion lost text")
		}
		img, err := render.NewDocument(color.White, ofd.Documents[0]).RasterizePage(page, render.BackendCanvas, geom.DPMM(4))
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []color.RGBA{{R: 63, B: 191, A: 255}, {R: 191, B: 63, A: 255}} {
			x := float64(50 + i*100)
			if shading {
				x = float64(60 + i*80)
			}
			y := 150 * pdfPointToMillimeter * 4
			got := img.RGBAAt(int(x*pdfPointToMillimeter*4), int(y))
			if math.Abs(float64(got.R)-float64(want.R)) > 4 || got.G > 4 || math.Abs(float64(got.B)-float64(want.B)) > 4 {
				t.Fatalf("shading=%v got=%v want=%v", shading, got, want)
			}
		}
	}
}

func TestPDFType4FailuresReportFunctionSource(t *testing.T) {
	for _, code := range []string{"{ unknown }", "{ 0 div dup dup }"} {
		var output bytes.Buffer
		var diagnostics []Diagnostic
		err := ConvertWithOptions(t.Context(), calculatorPDF(code, false), &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
		if !errors.Is(err, ErrUnsupported) || output.Len() != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "function.type4" || diagnostics[0].Page != 1 || diagnostics[0].Object != 5 || diagnostics[0].Resource != "Spot" {
			t.Fatalf("%s error=%v diagnostics=%+v output=%d", code, err, diagnostics, output.Len())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	err := ConvertWithOptions(ctx, calculatorPDF("{0 div}", false), &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel error=%v output=%d", err, output.Len())
	}
}

func TestPDFType4WorkDepthAndStackBounds(t *testing.T) {
	// 函数解析与求值共享文档处理预算，超限发生在写出之前。
	input := calculatorPDF("{ dup 0 exch 1 exch sub }", false)
	requireBudgetError(t, input, Limits{MaxOperations: 32}, "operations")
	nested := "{" + strings.Repeat(" {", 9) + " 1 " + strings.Repeat("}", 9) + "}"
	requireBudgetError(t, calculatorPDF(nested, false), Limits{MaxDepth: 8}, "function_depth")
	var diagnostics []Diagnostic
	fn := testCalculator(t, "{ "+strings.Repeat("dup ", calculatorStackLimit)+" }", []float64{0, 1}, []float64{0, 1}, Options{OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	if _, ok := fn.eval([]float64{0}); ok || len(diagnostics) != 1 {
		t.Fatalf("stack overflow ok=%v diagnostics=%v", ok, diagnostics)
	}
	// 编译过程与执行都检查 context，且取消错误保持原值。
	ctx, cancel := context.WithCancel(t.Context())
	opts, _ := (Options{}).Normalize()
	control := &conversionControl{ctx: ctx, options: opts}
	compiled, err := compilePDFCalculator([]byte("{dup}"), []float64{0, 1}, []float64{0, 1}, control)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, ok := compiled.eval([]float64{0}); ok || control.check() != context.Canceled {
		t.Fatalf("cancel=%v ok=%v", control.check(), ok)
	}
}

func TestPDFType4DeviceNAndMeshUseMultipleInputs(t *testing.T) {
	fn := testCalculator(t, "{ 2 copy add 2 div }", []float64{0, 1, 0, 1}, []float64{0, 1, 0, 1, 0, 1}, Options{Strict: true})
	space := &pdfColorSpace{family: "DeviceN", components: 2, base: deviceRGBSpace, tint: fn}
	got, ok := space.colorFromComponents([]float64{0.2, 0.8})
	if !ok || got != (pdfColor{r: 51, g: 204, b: 127}) {
		t.Fatalf("DeviceN=%v ok=%v", got, ok)
	}
	mesh := &pdfShading{colorSpace: deviceRGBSpace, functions: []*pdfFunction{fn}}
	got, ok = mesh.meshColorAt([]float64{0.2, 0.8})
	if !ok || got != (pdfColor{r: 51, g: 204, b: 127}) {
		t.Fatalf("mesh=%v ok=%v", got, ok)
	}
}

func TestPDFType4RuntimeBudgetCountsRepeatedEvaluations(t *testing.T) {
	fn := testCalculator(t, "{ dup mul }", []float64{0, 1}, []float64{0, 1}, Options{Limits: Limits{MaxOperations: 7}})
	if _, ok := fn.eval([]float64{0.5}); !ok {
		t.Fatal("first eval failed")
	}
	if _, ok := fn.eval([]float64{0.5}); ok {
		t.Fatal("repeated eval bypassed operation budget")
	}
	var limit *LimitError
	if err := fn.calculator.control.check(); !errors.As(err, &limit) || limit.Resource != "operations" {
		t.Fatalf("error=%v", err)
	}
}

func TestPDFType4RuntimeDepthStopsDuplicatedProcedureRecursion(t *testing.T) {
	// 过程通过 dup 将自身留在栈中再调用，源码只有两层花括号；执行深度也要检查。
	fn := testCalculator(t, "{ { dup true exch if } dup true exch if }", []float64{0, 1}, []float64{0, 1}, Options{Limits: Limits{MaxDepth: 8}})
	if _, ok := fn.eval([]float64{0}); ok {
		t.Fatal("recursive procedure bypassed depth budget")
	}
	var limit *LimitError
	if err := fn.calculator.control.check(); !errors.As(err, &limit) || limit.Resource != "function_depth" {
		t.Fatalf("error=%v", err)
	}
}

func TestPDFType4LenientRuntimeFailureKeepsColourAndReportsOnce(t *testing.T) {
	var diagnostics []Diagnostic
	fn := testCalculator(t, "{ 0 div }", []float64{0, 1}, []float64{0, 1}, Options{OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
	p := pdfInterpreter{state: pdfGraphicsState{fill: pdfColor{r: 255}}, control: fn.calculator.control}
	p.state.fillSpace = &pdfColorSpace{family: "Separation", components: 1, base: deviceGraySpace, tint: fn}
	for i := 0; i < 2; i++ {
		if err := p.operator("scn", []any{0.5}, nil, 0); err != nil {
			t.Fatal(err)
		}
		if p.state.fill != (pdfColor{r: 255}) {
			t.Fatalf("failed tint changed previous colour: %v", p.state.fill)
		}
	}
	if len(diagnostics) != 1 || p.control.check() != nil {
		t.Fatalf("diagnostics=%v error=%v", diagnostics, p.control.check())
	}
}
