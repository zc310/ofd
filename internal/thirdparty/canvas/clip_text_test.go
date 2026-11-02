package canvas

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

type clippedPathCapture struct {
	*Canvas

	path *Path
}

func (r *clippedPathCapture) RenderPath(path *Path, style Style, m Matrix) {
	r.path = path
	r.Canvas.RenderPath(path, style, m)
}

func TestContextClippedTextReplayPreservesNativeClipping(t *testing.T) {
	t.Parallel()

	family := NewFontFamily("context-clip")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	text := NewTextLine(family.Face(12, Black), "Hello", Left)
	clip := Rectangle(5, 4)
	matrix := Identity.Translate(2, 3).Shear(.2, .1)
	scene := New(30, 20)
	scene.RenderClippedText(text, matrix, clip)
	target := &clippedTextCapture{Canvas: New(30, 20), m: Identity}
	context := NewContext(NewContext(target))
	// Renderer calls already carry their transform; Context must not apply its view again.
	context.SetView(Identity.Scale(7, 9))
	view := Identity.Rotate(15)
	scene.RenderViewTo(context, view)
	if target.text != text || target.m != view.Mul(matrix) || !target.clip.Equals(clip) {
		t.Fatal("Context flattened clipped text or changed its transform or clip")
	}
	if !target.Empty() {
		t.Fatal("native clipped text was also emitted as outlines")
	}
}

func TestContextClippedTextFallbackKeepsClippedOutlines(t *testing.T) {
	t.Parallel()

	family := NewFontFamily("context-clip")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	text := NewTextLine(family.Face(12, Black), "Hello", Left)
	clip := Rectangle(5, 4)
	matrix := Identity.Translate(2, 3).Shear(.2, .1)
	scene := New(30, 20)
	scene.RenderClippedText(text, matrix, clip)
	target := New(30, 20)
	// Expose only Renderer, hiding Canvas's native clipping implementation.
	context := NewContext(struct{ Renderer }{target})
	scene.RenderTo(context)
	if target.Empty() || !clip.Equals(Rectangle(5, 4)) {
		t.Fatal("Context fallback produced no outlines or modified the shared clip")
	}
	for _, layers := range target.layers {
		for _, layer := range layers {
			if layer.path == nil || layer.text != nil {
				t.Fatal("fallback retained unclipped text instead of clipped outlines")
			}
		}
	}
}

func TestClippedTextFallbackBoundsOnlySkipsSafeIntersections(t *testing.T) {
	t.Parallel()

	path := Rectangle(4, 4).Translate(2, 2)
	for _, tc := range []struct {
		name      string
		clip      *Path
		unchanged bool
		empty     bool
	}{
		{name: "contained_rectangle", clip: Rectangle(10, 10), unchanged: true},
		{name: "outside", clip: Rectangle(1, 1), empty: true},
		{name: "rectangle_with_hole", clip: Rectangle(10, 10).Append(Rectangle(6, 6).Translate(1, 1).Reverse()), empty: true},
		{name: "partial_rectangle", clip: Rectangle(4, 10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := &clippedPathCapture{Canvas: New(20, 20)}
			r := clippedTextPaths{
				Renderer: target, clip: tc.clip, bounds: tc.clip.Bounds(),
				rectangle: tc.clip.IsAxisAlignedRect(), resolution: 0,
			}
			r.RenderPath(path, DefaultStyle, Identity)
			if (target.path == nil) != tc.empty {
				t.Fatalf("empty result=%v, want %v", target.path == nil, tc.empty)
			}
			if tc.unchanged && target.path != path {
				t.Fatal("contained path was copied or intersected")
			}
			if tc.name == "partial_rectangle" && target.path.Bounds().X1 > 4 {
				t.Fatal("partial clip was bypassed")
			}
		})
	}
}

func TestClippedTextFallbackClosesEveryFillSubpath(t *testing.T) {
	t.Parallel()

	path := MustParseSVGPath("M 0 0 L 4 0 L 4 4 L 0 4 M 6 0 L 10 0 L 10 4 L 6 4")
	before := path.Copy()
	clip := Rectangle(8, 3).Translate(1, .5)
	target := New(20, 20)
	r := clippedTextPaths{Renderer: target, clip: clip, bounds: clip.Bounds(), rectangle: true}
	r.RenderPath(path, DefaultStyle, Identity)
	if !path.Equals(before) {
		t.Fatal("closing subpaths modified the source")
	}
	if target.Empty() {
		t.Fatal("open filling subpaths disappeared")
	}
	result := target.layers[0][0].path
	if !result.ContainsPoint(2, 2, NonZero) || !result.ContainsPoint(8, 2, NonZero) || result.ContainsPoint(5, 2, NonZero) {
		t.Fatal("fill subpaths were not independently closed and clipped")
	}
}
