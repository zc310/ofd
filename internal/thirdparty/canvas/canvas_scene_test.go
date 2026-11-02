package canvas

import "testing"

// Embedding Renderer exposes the ordinary drawing operations, but deliberately
// hides the optional RenderCanvas method to exercise the replay fallback.
type sceneFallback struct{ Renderer }

func TestCanvasReusableScenePreservesTransformsAndReplay(t *testing.T) {
	child := New(10, 10)
	child.RenderPath(Rectangle(4, 3), DefaultStyle, Identity.Translate(1, 2))
	parent := New(30, 30)
	parent.RenderCanvas(child, Identity.Translate(5, 6))
	if got, want := parent.Bounds(), (Rect{6, 8, 10, 11}); got != want {
		t.Fatalf("nested bounds = %v, want %v", got, want)
	}
	replayed := New(30, 30)
	parent.RenderViewTo(sceneFallback{replayed}, Identity.Scale(2, 2))
	if got, want := replayed.Bounds(), (Rect{12, 16, 20, 22}); got != want {
		t.Fatalf("replayed bounds = %v, want %v", got, want)
	}
	if got := child.Bounds(); got != (Rect{1, 2, 5, 5}) {
		t.Fatalf("shared child was mutated: %v", got)
	}
	if parent.MemorySize() <= child.MemorySize() {
		t.Fatal("memory estimate omitted the retained scene")
	}
}
