package render

import (
	"testing"

	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
)

func TestTextHScaleDefaultsToOne(t *testing.T) {
	if got := textHScale(models.TextObject{}); got != 1 {
		t.Fatalf("expected default horizontal scale 1, got %v", got)
	}
	if got := textHScale(models.TextObject{CtText: models.CtText{HScale: 0.5}}); got != 0.5 {
		t.Fatalf("expected horizontal scale 0.5, got %v", got)
	}
}

func TestTextFillDisabled(t *testing.T) {
	if !textFillDisabled(models.TextObject{CtText: models.CtText{Fill: models.NewOptionalBool(false)}}) {
		t.Fatal("expected Fill=false to disable text fill")
	}
	if textFillDisabled(models.TextObject{CtText: models.CtText{Fill: models.NewOptionalBool(true)}}) {
		t.Fatal("expected Fill=true to keep text fill enabled")
	}
	if textFillDisabled(models.TextObject{}) {
		t.Fatal("expected missing Fill to keep text fill enabled")
	}
}

func TestNormalizeTextDirection(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 0},
		{90, 90},
		{180, 180},
		{270, 270},
		{360, 0},
		{-90, 270},
		{44, 0},
		{45, 90},
		{315, 0},
	}
	for _, test := range tests {
		if got := normalizeTextDirection(test.input); got != test.want {
			t.Errorf("normalizeTextDirection(%d) = %d, want %d", test.input, got, test.want)
		}
	}
}

func TestTextReadAdvance(t *testing.T) {
	for _, test := range []struct {
		direction int
		wantX     float64
		wantY     float64
	}{
		{0, 3, 0},
		{90, 0, 3},
		{180, -3, 0},
		{270, 0, -3},
	} {
		gotX, gotY := textReadAdvance(3, test.direction)
		if gotX != test.wantX || gotY != test.wantY {
			t.Errorf("textReadAdvance(%d) = (%v, %v), want (%v, %v)", test.direction, gotX, gotY, test.wantX, test.wantY)
		}
	}
}

func TestTextCharDirectionDegrees(t *testing.T) {
	for _, test := range []struct {
		direction int
		want      float64
	}{
		{0, 0},
		{90, 90},
		{180, 180},
		{270, 270},
		{-90, 270},
		{450, 90},
	} {
		object := models.TextObject{CtText: models.CtText{CharDirection: test.direction}}
		if got := textCharDirectionDegrees(object); got != test.want {
			t.Errorf("textCharDirectionDegrees(%d) = %v, want %v", test.direction, got, test.want)
		}
	}
}

func TestTextAdvanceUsesExplicitDeltasBeforeReadDirection(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{ReadDirection: 90}}
	code := models.TextCode{DeltaX: models.StArrayF{2}, DeltaY: models.StArrayF{4}}
	gotX, gotY := textAdvance(10, object, code, 0)
	if gotX != 2 || gotY != 4 {
		t.Fatalf("textAdvance with explicit deltas = (%v, %v), want (2, 4)", gotX, gotY)
	}

	code = models.TextCode{}
	gotX, gotY = textAdvance(10, object, code, 0)
	if gotX != 0 || gotY != 10 {
		t.Fatalf("textAdvance with ReadDirection=90 = (%v, %v), want (0, 10)", gotX, gotY)
	}
}

func TestBuildTextLayoutUsesFallbackGlyphWidthsAndDirections(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4}},
		Size:          4,
		HScale:        0.5,
		ReadDirection: 90,
	}}
	layout := buildTextLayout(nil, object, models.TextCode{Value: "ab", X: 1, Y: 4}, 0)
	if len(layout.Glyphs) != 2 {
		t.Fatalf("glyph count = %d, want 2", len(layout.Glyphs))
	}
	if layout.Glyphs[0].X != 11 || layout.Glyphs[0].Y != 20 {
		t.Fatalf("first glyph = %+v, want x=11 y=20", layout.Glyphs[0])
	}
	if layout.Glyphs[0].Width != 3 || layout.Glyphs[0].Height != 4 {
		t.Fatalf("first glyph size = %.2fx%.2f, want 3x4", layout.Glyphs[0].Width, layout.Glyphs[0].Height)
	}
	if layout.Glyphs[1].X != 11 || layout.Glyphs[1].Y != 23 {
		t.Fatalf("second glyph = %+v, want x=11 y=23", layout.Glyphs[1])
	}
}

func TestBuildTextLayoutUsesExplicitDeltas(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4}},
		Size:          4,
		ReadDirection: 90,
	}}
	layout := buildTextLayout(nil, object, models.TextCode{
		Value:  "ab",
		X:      1,
		Y:      4,
		DeltaX: models.StArrayF{2},
		DeltaY: models.StArrayF{3},
	}, 0)
	if len(layout.Glyphs) != 2 {
		t.Fatalf("glyph count = %d, want 2", len(layout.Glyphs))
	}
	if layout.Glyphs[1].X != 13 || layout.Glyphs[1].Y != 23 {
		t.Fatalf("second glyph = %+v, want x=13 y=23", layout.Glyphs[1])
	}
}

func TestBuildTextLayoutAppliesCTMScaleToGlyphGeometry(t *testing.T) {
	ctm := models.CTM{2, 0, 0, 3, 0, 0}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4},
			CTM:      &ctm,
		},
		Size: 4,
	}}
	layout := buildTextLayout(nil, object, models.TextCode{Value: "ab", X: 1, Y: 4}, 0)
	if len(layout.Glyphs) != 2 {
		t.Fatalf("glyph count = %d, want 2", len(layout.Glyphs))
	}
	if layout.Glyphs[0].X != 12 || layout.Glyphs[0].Y != 20 || layout.Glyphs[0].Width != 6 || layout.Glyphs[0].Height != 12 {
		t.Fatalf("first glyph = %+v, want x=12 y=20 width=6 height=12", layout.Glyphs[0])
	}
	if layout.Glyphs[1].X != 24 || layout.Glyphs[1].Y != 20 {
		t.Fatalf("second glyph = %+v, want x=24 y=20", layout.Glyphs[1])
	}
}

func TestApplyCGTransformWidthsUsesMappedGlyphs(t *testing.T) {
	widths := []float64{2, 3, 4}
	runes := []rune("abc")
	widthOf := func(value string) float64 {
		if value == string(fontfix.GlyphRune(65)) {
			return 9
		}
		if value == string(fontfix.GlyphRune(66)) {
			return 6
		}
		return 0
	}
	applyCGTransformWidths(widths, runes, []models.CTCGTransform{{
		CodePosition: 1,
		CodeCount:    2,
		GlyphCount:   2,
		Glyphs:       models.StArrayI{65, 66},
	}}, 0, widthOf)
	if widths[0] != 2 || widths[1] != 9 || widths[2] != 6 {
		t.Fatalf("mapped widths = %v, want [2 9 6]", widths)
	}
}

func TestTextCodeGlyphsClampsExcessiveCodeCount(t *testing.T) {
	runes := []rune("ab")
	glyphs := textCodeGlyphs(nil, runes, []models.CTCGTransform{{
		CodePosition: 0,
		CodeCount:    int(^uint(0) >> 1),
		Glyphs:       []int{65},
	}}, 0)

	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %+v, want one mapped glyph", glyphs)
	}
}

func TestRenderableTextValueSkipsControlCharacters(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"A", true},
		{"ą", true},
		{"\u009b", false},
		{"\u0000", false},
		{"\u007f", false},
		{"\uFFFD", false},
		{"", false},
		{"a\u009b", true},
	}
	for _, tc := range cases {
		if got := renderableTextValue(tc.value); got != tc.want {
			t.Fatalf("renderableTextValue(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
