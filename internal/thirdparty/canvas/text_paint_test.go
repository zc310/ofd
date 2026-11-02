//nolint:testpackage // Replay assertions inspect private recorded layers and paint metadata.
package canvas

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

type textPaintCapture struct {
	*clippedTextCapture

	accepted bool
	paint    Paint
	paintM   Matrix
	calls    int
}

func (r *textPaintCapture) RenderTextWithPaint(
	text *Text,
	matrix Matrix,
	clip *Path,
	paint Paint,
	paintMatrix Matrix,
) bool {
	r.calls++

	r.paint, r.paintM = paint, paintMatrix
	if r.accepted {
		r.text, r.m, r.clip = text, matrix, clip
	}

	return r.accepted
}

func sharedPaintTestText(t *testing.T) *Text {
	t.Helper()

	family := NewFontFamily(t.Name())

	err := family.LoadFont(goregular.TTF, 0, FontRegular)
	if err != nil {
		t.Fatal(err)
	}

	return NewTextLine(family.Face(12, Black), "Hello", Left)
}

func TestSharedTextPaintTransformsBothSpacesAndCopiesClip(t *testing.T) {
	t.Parallel()

	text := sharedPaintTestText(t)
	clip := Rectangle(5, 4)
	matrix := Identity.Translate(2, 3).Shear(.2, .1)
	paintM := Identity.Translate(7, 8).Scale(2, 3)
	paint := DefaultStyle.Fill
	scene := New(30, 20)
	scene.RenderTextWithPaint(text, matrix, clip, paint, paintM)
	clip.Translate(100, 100)

	transform := Identity.Rotate(15)
	scene.Transform(transform)

	target := new(textPaintCapture)
	target.clippedTextCapture = &clippedTextCapture{Canvas: New(30, 20), text: nil, clip: nil, m: Identity}
	target.accepted = true
	context := NewContext(target)
	context.SetView(Identity.Scale(7, 9))
	view := Identity.Translate(4, 5)
	scene.RenderViewTo(context, view)

	if target.calls != 1 || target.text != text || target.m != view.Mul(transform).Mul(matrix) {
		t.Fatal("shared paint changed text replay or applied the Context view twice")
	}

	if target.paint != paint || target.paintM != view.Mul(transform).Mul(paintM) {
		t.Fatal("paint did not follow the same outer transforms as text")
	}

	if target.clip == nil || !target.clip.Equals(Rectangle(5, 4)) {
		t.Fatal("recording modified or retained the caller's clip")
	}

	if !target.Empty() {
		t.Fatal("native paint replay also emitted ordinary text")
	}
}

func TestRejectedSharedTextPaintReplaysOriginalSpansAndClip(t *testing.T) {
	t.Parallel()

	text := sharedPaintTestText(t)
	clip := Rectangle(5, 4)
	matrix := Identity.Translate(2, 3)
	scene := New(30, 20)
	scene.RenderTextWithPaint(text, matrix, clip, DefaultStyle.Fill, Identity)

	target := new(textPaintCapture)
	target.clippedTextCapture = &clippedTextCapture{Canvas: New(30, 20), text: nil, clip: nil, m: Identity}
	scene.RenderTo(target)

	if target.calls != 1 || target.text != text || target.m != matrix || !target.clip.Equals(clip) {
		t.Fatal("rejected shared paint did not retain the ordinary text and native clip")
	}

	text.WalkSpans(func(_, _ float64, span TextSpan) {
		if span.Face.Fill != DefaultStyle.Fill {
			t.Error("recording modified a shared span paint")
		}
	})
}

func TestSharedTextPaintAccountsForRetentionAndReset(t *testing.T) {
	t.Parallel()

	text := sharedPaintTestText(t)
	ordinary, shared := New(30, 20), New(30, 20)
	ordinary.RenderText(text, Identity)
	shared.RenderTextWithPaint(text, Identity, nil, DefaultStyle.Fill, Identity)

	if shared.MemorySize() <= ordinary.MemorySize() {
		t.Fatal("paint metadata was omitted from retained memory accounting")
	}

	shared.Reset()

	if shared.MemorySize() != New(30, 20).MemorySize() {
		t.Fatal("Reset retained shared paint metadata")
	}
}
