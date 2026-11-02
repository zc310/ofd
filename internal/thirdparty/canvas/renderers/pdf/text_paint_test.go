//nolint:testpackage // Tests assert private Pattern resources and unchanged text matrices.
package pdf

import (
	"bytes"
	"image/color"
	"regexp"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas" //nolint:depguard // Tests exercise the canvas types consumed by the PDF renderer.
)

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
