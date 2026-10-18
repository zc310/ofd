package models

import "testing"

func TestExpandCGTransformPositionsForTextCodeLocalOffsets(t *testing.T) {
	codes := []TextCode{{Value: "电"}, {Value: "子"}, {Value: "E"}}
	transforms := []CTCGTransform{
		{CodePosition: 0, CodeCount: 1, GlyphCount: 1, Glyphs: StArrayI{14}},
		{CodePosition: 0, CodeCount: 1, GlyphCount: 1, Glyphs: StArrayI{15}},
		{CodePosition: 0, CodeCount: 1, GlyphCount: 1, Glyphs: StArrayI{29}},
	}

	got := ExpandCGTransformPositions(codes, transforms)
	for i, want := range []int{0, 1, 2} {
		if got[i].CodePosition != want {
			t.Errorf("transform %d position = %d, want %d", i, got[i].CodePosition, want)
		}
	}
	if transforms[1].CodePosition != 0 {
		t.Fatal("input transforms were modified")
	}
}

func TestExpandCGTransformPositionsKeepsGlobalOffsets(t *testing.T) {
	codes := []TextCode{{Value: "ab"}, {Value: "cd"}}
	transforms := []CTCGTransform{
		{CodePosition: 0},
		{CodePosition: 2},
	}
	got := ExpandCGTransformPositions(codes, transforms)
	if got[0].CodePosition != 0 || got[1].CodePosition != 2 {
		t.Fatalf("global positions changed: got %d, %d", got[0].CodePosition, got[1].CodePosition)
	}
}
