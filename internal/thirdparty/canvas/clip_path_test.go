//nolint:testpackage // Recording checks use the internal outline fallback independently of native replay.
package canvas

import "testing"

type pathClipFallbackCapture struct {
	Renderer

	paths  []*Path
	styles []Style
	matrix Matrix
}

func (r *pathClipFallbackCapture) RenderPath(path *Path, style Style, matrix Matrix) {
	r.paths = append(r.paths, path)
	r.styles = append(r.styles, style)
	r.matrix = matrix
}

type pathClipNativeCapture struct {
	*pathClipFallbackCapture

	path  *Path
	clip  *Path
	style Style
}

func (r *pathClipNativeCapture) RenderClippedPath(path *Path, style Style, matrix Matrix, clip *Path) {
	r.path, r.clip, r.style, r.matrix = path, clip, style, matrix
}

func TestClippedPathCopiesGeometryAndDashesAndTransformsBothSpaces(t *testing.T) {
	t.Parallel()

	path := Circle(5).Translate(10, 10)
	clip := Rectangle(10, 10).Translate(10, 10)
	before, clipBefore := path.Copy(), clip.Copy()
	style := DefaultStyle
	style.Stroke, style.Dashes = DefaultStyle.Fill, []float64{2, 1}
	matrix := Identity.Translate(2, 3).Shear(.2, .1)
	scene := New(30, 30)
	scene.RenderClippedPath(path, style, matrix, clip)
	path.Translate(100, 100)
	clip.Translate(100, 100)

	style.Dashes[0] = 99
	transform, view := Identity.Rotate(15), Identity.Translate(4, 5)
	scene.Transform(transform)

	target := &pathClipNativeCapture{
		pathClipFallbackCapture: &pathClipFallbackCapture{Renderer: New(30, 30), paths: nil, styles: nil, matrix: Identity},
		path:                    nil, clip: nil, style: DefaultStyle,
	}
	context := NewContext(target)
	context.SetView(Identity.Scale(7, 9))
	scene.RenderViewTo(context, view)

	if !target.path.Equals(before) || !target.clip.Equals(clipBefore) || target.style.Dashes[0] != 2 {
		t.Fatal("recording retained mutable source geometry or dashes")
	}

	if target.matrix != view.Mul(transform).Mul(matrix) || len(target.paths) != 0 {
		t.Fatal("native replay changed the transform or also emitted fallback outlines")
	}

	ordinary := New(30, 30)
	ordinary.RenderPath(before, style, matrix)

	if scene.MemorySize() <= ordinary.MemorySize() {
		t.Fatal("clip and dash storage were omitted from memory accounting")
	}

	scene.Reset()

	if scene.MemorySize() != New(30, 30).MemorySize() {
		t.Fatal("Reset retained clipped path data")
	}
}

func TestClippedPathFallbackPreservesEvenOddFillAndAbsoluteDashGeometry(t *testing.T) {
	t.Parallel()

	path := Rectangle(20, 20).Append(Rectangle(8, 8).Translate(6, 6))
	clip := Rectangle(16, 16).Translate(2, 2)
	style := DefaultStyle
	style.FillRule, style.Stroke, style.StrokeWidth = EvenOdd, style.Fill, 2
	style.DashOffset, style.Dashes = .25, []float64{2, 1}
	matrix := Identity.Translate(5, 5).Shear(.2, .1)
	scene := New(30, 30)
	scene.RenderClippedPath(path, style, matrix, clip)
	target := &pathClipFallbackCapture{Renderer: New(30, 30), paths: nil, styles: nil, matrix: Identity}
	scene.RenderTo(target)

	offset, dashes := ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
	wantFill := closedTextFill(path, EvenOdd).And(clip)

	wantStroke := path.Dash(offset, dashes...).Stroke(2, style.StrokeCapper, style.StrokeJoiner, Tolerance).And(clip)
	if len(target.paths) != 2 || !target.paths[0].Equals(wantFill) || !target.paths[1].Equals(wantStroke) {
		t.Fatal("fallback changed holes, dash lengths, phase or stroke clipping")
	}

	if target.matrix != matrix || target.styles[0].FillRule != NonZero || target.styles[1].HasStroke() {
		t.Fatal("fallback did not emit independently filled clipped outlines")
	}
}
