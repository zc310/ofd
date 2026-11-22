//nolint:testpackage // Tests assert private Pattern resources and unchanged text matrices.
package pdf

import (
	"bytes"
	"image/color"
	"reflect"
	"regexp"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas" //nolint:depguard // Tests exercise the canvas types consumed by the PDF renderer.
)

func TestSyntheticBoldRestoresGraphicsState(t *testing.T) {
	t.Parallel()
	family := sharedPaintFamily(t)
	var output bytes.Buffer
	renderer := New(&output, 40, 20, &Options{})
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: color.RGBA{R: 128, A: 128}}
	style.Stroke = canvas.Paint{Color: canvas.Black}
	style.StrokeWidth = 0.7
	renderer.RenderPath(canvas.Rectangle(2, 2), style, canvas.Identity)
	before := renderer.w.pdfGraphicsState
	face := family.Face(24, canvas.Black, canvas.FontBold)
	renderer.RenderText(canvas.NewTextLine(face, "Bold", canvas.Left), canvas.Identity.Translate(5, 8).Scale(2, 0.8))
	// q/Q 必须同时恢复 PDF 图形状态及写入器缓存，后续相同画笔才能安全复用。
	if !reflect.DeepEqual(before, renderer.w.pdfGraphicsState) {
		t.Fatal("合成粗体改变了后续图元的图形状态缓存")
	}
	renderer.RenderPath(canvas.Rectangle(2, 2), style, canvas.Identity.Translate(3, 0))
	closeFunctionWriter(t, renderer)
}

// 未装饰文字不应为了装饰阶段预先计算字形轮廓，否则合成粗体会重复执行昂贵的
// 曲线偏移。这里确认普通文字仍按原生合成粗体写入，且真实装饰不会被误跳过。
func TestRenderTextWithoutDecorationsSkipsDecorationOutlinePass(t *testing.T) {
	t.Parallel()

	family := sharedPaintFamily(t)
	face := family.Face(16, canvas.Black, canvas.FontBold)
	if face.FauxBold <= 0 {
		t.Fatal("测试字体未启用合成粗体")
	}
	text := canvas.NewTextLine(face, "Bold", canvas.Left)
	matrix := canvas.Identity.Translate(5, 20)

	var plain bytes.Buffer
	renderer := New(&plain, 80, 40, &Options{})
	func() {
		// 独立字体尚未生成轮廓缓存。暂时移除已解析的轮廓表，使任何多余的
		// GlyphPath 调用立即失败；原生文字只需字形与度量，序列化前恢复字体。
		outlines := face.Font.Glyf
		face.Font.Glyf = nil
		defer func() {
			face.Font.Glyf = outlines
			if recovered := recover(); recovered != nil {
				t.Fatalf("无装饰原生文字仍访问了字形轮廓表: %v", recovered)
			}
		}()
		renderer.RenderText(text, matrix)
	}()
	closeFunctionWriter(t, renderer)

	if !bytes.Contains(plain.Bytes(), []byte("2 Tr")) || !bytes.Contains(plain.Bytes(), []byte("Tf")) {
		t.Fatal("未装饰文字没有按原生合成粗体写入")
	}

	decorated := canvas.NewTextLine(family.Face(16, canvas.Black, canvas.FontBold, canvas.FontUnderline), "Bold", canvas.Left)
	var withUnderline bytes.Buffer
	underlined := New(&withUnderline, 80, 40, &Options{})
	underlined.RenderText(decorated, matrix)

	// 显式绘制真实下划线再写入原生文字，作为装饰内容的精确参照，
	// 不能只凭两份 PDF 不同就断定装饰确实存在。
	var expected bytes.Buffer
	reference := New(&expected, 80, 40, &Options{})
	decorated.RenderDecorationsTo(reference, matrix, 0)
	if reference.w.Len() == 0 {
		t.Fatal("参照下划线没有生成绘制命令")
	}
	reference.RenderText(text, matrix)
	if !bytes.Equal(underlined.w.Bytes(), reference.w.Bytes()) {
		t.Fatal("装饰文字未保留真实下划线与原生文字绘制命令")
	}
	closeFunctionWriter(t, reference)
	closeFunctionWriter(t, underlined)
	if bytes.Equal(plain.Bytes(), withUnderline.Bytes()) {
		t.Fatal("装饰文字与未装饰文字输出相同，装饰阶段可能被错误跳过")
	}
}

func sharedPaintFamily(t *testing.T) *canvas.FontFamily {
	t.Helper()

	family := canvas.NewFontFamily(t.Name())

	err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular)
	if err != nil {
		t.Fatal(err)
	}

	return family
}

func TestSharedTextPaintReusesPatternWithoutChangingGlyphPlacement(t *testing.T) {
	t.Parallel()

	family := sharedPaintFamily(t)

	var (
		before, after bytes.Buffer
		options       Options
	)

	original, optimized := New(&before, 40, 20, &options), New(&after, 40, 20, &options)
	gradient := canvas.NewLinearGradient(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 20, Y: 0})
	gradient.Grad = sharedFunctionStops()
	paint := canvas.DefaultStyle.Fill
	paint.Gradient = gradient

	for _, x := range []float64{1, 9.25, 18.125} {
		local := canvas.NewLinearGradient(canvas.Point{X: -x, Y: 0}, canvas.Point{X: 20 - x, Y: 0})
		local.Grad = sharedFunctionStops()
		face := family.Face(12, local)
		text := canvas.NewTextLine(face, "Hello", canvas.Left)
		matrix := canvas.Identity.Translate(x, 8)
		clip := canvas.Rectangle(7, 5).Translate(0, -1)
		original.RenderClippedText(text, matrix, clip)

		if !optimized.RenderTextWithPaint(text, matrix, clip, paint, canvas.Identity.Translate(0, 8)) {
			t.Fatal("opaque native gradient text was rejected")
		}
	}

	positions := regexp.MustCompile(`[-.0-9 ]+Tm`)
	oldPositions := bytes.Join(positions.FindAll(original.w.Bytes(), -1), nil)

	newPositions := bytes.Join(positions.FindAll(optimized.w.Bytes(), -1), nil)
	if !bytes.Equal(oldPositions, newPositions) {
		t.Fatal("sharing a paint space changed glyph positioning matrices")
	}

	closeFunctionWriter(t, original)
	closeFunctionWriter(t, optimized)

	oldPatterns := bytes.Count(before.Bytes(), []byte("/PatternType 2"))

	newPatterns := bytes.Count(after.Bytes(), []byte("/PatternType 2"))
	if oldPatterns != 3 || newPatterns != 1 {
		t.Fatal("independently positioned text did not share exactly one Pattern")
	}
}

func TestSharedTextPaintRejectsTransparencyAndSyntheticWeightWithoutWriting(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"translucent", "synthetic_weight", "invisible"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			// Font shapers have mutable caches; each parallel case owns its font.
			family := sharedPaintFamily(t)

			var (
				output    bytes.Buffer
				options   Options
				invisible canvas.Paint
			)

			renderer := New(&output, 40, 20, &options)
			gradient := canvas.NewLinearGradient(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 20, Y: 0})
			gradient.Grad = sharedFunctionStops()
			face := family.Face(12, gradient)

			switch kind {
			case "translucent":
				gradient.Grad[0].Color = color.RGBA{R: 128, G: 0, B: 0, A: 128}
			case "synthetic_weight":
				face.FauxBold = .02
			default:
				face.Fill = invisible
			}

			text := canvas.NewTextLine(face, "Hello", canvas.Left)
			paint := canvas.DefaultStyle.Fill
			paint.Gradient = gradient

			before := bytes.Clone(renderer.w.Bytes())
			if renderer.RenderTextWithPaint(text, canvas.Identity, nil, paint, canvas.Identity) {
				t.Fatal("unsupported text was not left for ordinary replay")
			}

			if !bytes.Equal(before, renderer.w.Bytes()) || len(renderer.w.patterns) != 0 {
				t.Fatal("rejected paint-space optimization wrote partial content")
			}

			closeFunctionWriter(t, renderer)
		})
	}
}
