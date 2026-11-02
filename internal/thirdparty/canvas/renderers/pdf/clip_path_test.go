//nolint:testpackage // Assertions inspect native operators and the complete graphics-state cache.
package pdf

import (
	"bytes"
	"image/color"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas" //nolint:depguard // Tests exercise renderer input geometry.
)

func TestClippedPathKeepsNativeCurveDashAndRestoresGraphicsState(t *testing.T) {
	t.Parallel()

	var (
		output  bytes.Buffer
		options Options
		noPaint canvas.Paint
	)

	renderer := New(&output, 40, 30, &options)
	style := canvas.DefaultStyle

	style.Fill = noPaint
	style.Stroke = canvas.Paint{Color: color.RGBA{R: 128, G: 0, B: 0, A: 128}, Gradient: nil, Pattern: nil}
	style.StrokeWidth, style.DashOffset, style.Dashes = 2, .25, []float64{2, 1}
	path := canvas.MustParseSVGPath("M0 5 C5 20 15 -10 20 5")
	clip := canvas.Rectangle(10, 10).Translate(5, 0)
	beforePath, beforeClip := path.ToSVG(), clip.ToSVG()
	before := renderer.w.pdfGraphicsState
	matrix := canvas.Identity.Translate(3, 4).Rotate(15)
	start := renderer.w.Len()
	renderer.RenderClippedPath(path, style, matrix, clip)

	content := renderer.w.Bytes()[start:]
	if !bytes.Contains(content, []byte(" W n ")) || !bytes.Contains(content, []byte(" c S")) ||
		!bytes.Contains(content, []byte("[4 2] .5 d")) || !bytes.HasSuffix(content, []byte(" Q")) {
		t.Fatalf("native clipping, curve or dash operators missing: %s", content)
	}

	if !reflect.DeepEqual(before, renderer.w.pdfGraphicsState) {
		t.Fatal("q/Q failed to restore cached paint, opacity, width, joins or dashes")
	}

	if path.ToSVG() != beforePath || clip.ToSVG() != beforeClip {
		t.Fatal("native clipping mutated caller geometry")
	}

	start = renderer.w.Len()
	renderer.RenderPath(path, style, matrix)

	if !bytes.Contains(renderer.w.Bytes()[start:], []byte("[4 2] .5 d")) {
		t.Fatal("following stroke incorrectly reused dash settings restored by Q")
	}

	closeFunctionWriter(t, renderer)
}
