//nolint:testpackage // Resource-reference assertions require private PDF writer state.
package pdf

import (
	"bytes"
	"errors"
	"image/color"
	"io"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas" //nolint:depguard // These tests exercise canvas gradients consumed by this renderer.
)

func sharedFunctionStops() canvas.Grad {
	return canvas.Grad{
		{Offset: 0, Color: canvas.Red},
		{Offset: 0.4, Color: canvas.Green},
		{Offset: 1, Color: canvas.Blue},
	}
}

func patternFunctionRef(t *testing.T, page *pdfPageWriter, name pdfName) (pdfRef, pdfDict) {
	t.Helper()

	patterns, patternsOK := page.resources["Pattern"].(pdfDict)
	if !patternsOK {
		t.Fatal("missing Pattern resource dictionary")
	}

	pattern, patternOK := patterns[name].(pdfDict)
	if !patternOK {
		t.Fatal("missing gradient Pattern")
	}

	shading, shadingOK := pattern["Shading"].(pdfDict)
	if !shadingOK {
		t.Fatal("missing Shading dictionary")
	}

	ref, ok := shading["Function"].(pdfRef)
	if !ok {
		t.Fatal("gradient function is not a shared indirect reference")
	}

	return ref, shading
}

func closeFunctionWriter(t *testing.T, writer interface{ Close() error }) {
	t.Helper()

	err := writer.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func assertFunctionCacheReleased(t *testing.T, writer *pdfWriter) {
	t.Helper()

	if writer.gradientFunctions != nil || writer.gradientFunctionBytes != 0 {
		t.Fatal("Close retained the function cache")
	}
}

func assertRadialFunctionGeometry(t *testing.T, shading pdfDict, radial *canvas.RadialGradient) {
	t.Helper()

	coords := pdfArray{
		radial.C0.X * ptPerMm, radial.C0.Y * ptPerMm, radial.R0 * ptPerMm,
		radial.C1.X * ptPerMm, radial.C1.Y * ptPerMm, radial.R1 * ptPerMm,
	}
	if !reflect.DeepEqual(shading["Coords"], coords) {
		t.Fatal("function reuse changed radial geometry")
	}

	if !reflect.DeepEqual(shading["Extend"], pdfArray{false, true}) {
		t.Fatal("function reuse changed Extend settings")
	}
}

func TestGradientFunctionSharedAcrossGeometryPagesAndForms(t *testing.T) {
	t.Parallel()

	var (
		output  bytes.Buffer
		options Options
	)

	renderer := New(&output, 40, 30, &options)
	writer := renderer.w.pdf
	linear := canvas.NewLinearGradient(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 20, Y: 0})
	linear.Grad = sharedFunctionStops()
	firstName := renderer.w.getPattern(linear, canvas.Identity)
	first, _ := patternFunctionRef(t, renderer.w, firstName)

	// A shared stop function does not require the same matrix, geometry or shading type.
	radial := canvas.NewRadialGradient(canvas.Point{X: 2, Y: 3}, 1, canvas.Point{X: 5, Y: 6}, 10)
	radial.Grad = sharedFunctionStops()
	radial.Extend = [2]bool{false, true}
	name := renderer.w.getPattern(radial, canvas.Identity.Translate(7, 8).Scale(2, 3))

	ref, shading := patternFunctionRef(t, renderer.w, name)
	if name == firstName || ref != first {
		t.Fatal("Patterns with different geometry must stay separate but share the same stop function")
	}

	assertRadialFunctionGeometry(t, shading, radial)

	// Sharing a function must not merge Patterns with different Extend settings.
	other := *linear
	other.Extend = [2]bool{false, false}
	otherName := renderer.w.getPattern(&other, canvas.Identity)

	ref, shading = patternFunctionRef(t, renderer.w, otherName)
	if otherName == firstName || ref != first || !reflect.DeepEqual(shading["Extend"], pdfArray{false, false}) {
		t.Fatal("Pattern deduplication ignored Extend settings")
	}

	renderer.NewPage(40, 30)

	ref, _ = patternFunctionRef(t, renderer.w, renderer.w.getPattern(linear, canvas.Identity.Translate(3, 4)))
	if ref != first {
		t.Fatal("a new page did not reuse the document's stop function")
	}

	scene := canvas.New(10, 10)
	style := canvas.DefaultStyle
	style.Fill.Gradient = linear
	scene.RenderPath(canvas.Rectangle(10, 10), style, canvas.Identity)
	renderer.RenderCanvas(scene, canvas.Identity)

	if len(writer.gradientFunctions) != 1 {
		t.Fatal("Form did not reuse the document's stop function")
	}

	closeFunctionWriter(t, renderer)

	if bytes.Count(output.Bytes(), []byte("/FunctionType 3")) != 1 {
		t.Fatal("stop function dictionary was serialized more than once")
	}

	assertFunctionCacheReleased(t, writer)
}

func TestGradientFunctionDistinguishesColorsOffsetsAndDomain(t *testing.T) {
	t.Parallel()

	writer := newPDFWriter(io.Discard)
	base := sharedFunctionStops()

	first := writer.gradientFunction(base)
	if got := writer.gradientFunction(append(canvas.Grad(nil), base...)); got != first {
		t.Fatal("equal stops in distinct slices did not share a function")
	}

	for _, change := range []string{"color", "offset", "domain"} {
		grad := append(canvas.Grad(nil), base...)

		switch change {
		case "color":
			grad[1].Color = canvas.White
		case "offset":
			grad[1].Offset = 0.6
		case "domain":
			grad[0].Offset = 0.1
		}

		if got := writer.gradientFunction(grad); got == first {
			t.Fatalf("stop functions with different %s values were merged", change)
		}
	}

	if got := writer.gradientFunction(base); got != first {
		t.Fatal("later function construction modified a cached function")
	}

	closeFunctionWriter(t, writer)
}

func TestGradientFunctionWithManyStopsIsShared(t *testing.T) {
	t.Parallel()

	writer := newPDFWriter(io.Discard)
	// Stop lists larger than the fixed-size Pattern key can still share a function.
	many := make(canvas.Grad, 12)
	for i := range many {
		many[i] = canvas.Stop{Offset: float64(i) / float64(len(many)-1), Color: color.RGBA{R: uint8(i), G: 0, B: 0, A: 255}}
	}

	if a, b := writer.gradientFunction(many), writer.gradientFunction(append(canvas.Grad(nil), many...)); a != b {
		t.Fatal("function with many stops was not reused")
	}

	closeFunctionWriter(t, writer)
}

func TestGradientFunctionCacheRespectsCountAndByteBudgets(t *testing.T) {
	t.Parallel()

	writer := newPDFWriter(io.Discard)
	grad := sharedFunctionStops()

	for i := range maxGradientFunctions + 1 {
		grad[1].Offset = float64(i+1) / float64(maxGradientFunctions+2)
		writer.gradientFunction(grad)

		if len(writer.gradientFunctions) > maxGradientFunctions || writer.gradientFunctionBytes > maxGradientFunctionBytes {
			t.Fatal("stop function cache exceeded its budget")
		}
	}

	count, size := len(writer.gradientFunctions), writer.gradientFunctionBytes

	large := make(canvas.Grad, 20_000)
	for i := range large {
		large[i] = canvas.Stop{Offset: float64(i) / float64(len(large)-1), Color: canvas.Red}
	}

	if a, b := writer.gradientFunction(large), writer.gradientFunction(large); a == b {
		t.Fatal("oversized stop functions must not be retained")
	}

	if len(writer.gradientFunctions) != count || writer.gradientFunctionBytes != size {
		t.Fatal("oversized function evicted cached entries or was retained")
	}

	closeFunctionWriter(t, writer)
}

type gradientErrorWriter struct{ err error }

func (w gradientErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestGradientFunctionCacheReleasedWhenCloseFails(t *testing.T) {
	t.Parallel()

	want := io.ErrClosedPipe
	writer := newPDFWriter(gradientErrorWriter{err: want})
	writer.gradientFunction(sharedFunctionStops())

	err := writer.Close()
	if !errors.Is(err, want) {
		t.Fatalf("write error = %v, want %v", err, want)
	}

	assertFunctionCacheReleased(t, writer)
}

func TestGradientFunctionReferencesAreDocumentScoped(t *testing.T) {
	t.Parallel()

	// References cannot be reused across documents, even with identical stops.
	for range 2 {
		var output bytes.Buffer

		writer := newPDFWriter(&output)
		writer.gradientFunction(sharedFunctionStops())
		closeFunctionWriter(t, writer)

		if bytes.Count(output.Bytes(), []byte("/FunctionType 3")) != 1 {
			t.Fatal("each PDF must write its own stop function")
		}
	}
}
