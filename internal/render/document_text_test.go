package render

import (
	"image/color"
	"os"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
)

func TestTextFontStyle(t *testing.T) {
	tests := []struct {
		weight int
		want   canvas.FontStyle
	}{
		{0, canvas.FontRegular},
		{100, canvas.FontThin},
		{200, canvas.FontExtraLight},
		{300, canvas.FontLight},
		{400, canvas.FontRegular},
		{500, canvas.FontMedium},
		{600, canvas.FontSemiBold},
		{700, canvas.FontBold},
		{800, canvas.FontExtraBold},
		{900, canvas.FontBlack},
		{1000, canvas.FontBlack},
	}
	for _, test := range tests {
		if got := textFontStyle(test.weight, false); got != test.want {
			t.Errorf("weight %d: got %v, want %v", test.weight, got, test.want)
		}
	}
	if got := textFontStyle(700, true); got != canvas.FontBold|canvas.FontItalic {
		t.Fatalf("italic bold: got %v", got)
	}
}

func TestBuildTextFaceUsesGradientFill(t *testing.T) {
	family := canvas.NewFontFamily("test")
	if err := family.LoadSystemFont("DejaVu Sans", canvas.FontRegular); err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	gradient := canvas.NewLinearGradient(canvas.Point{X: 0, Y: 0}, canvas.Point{X: 10, Y: 0})
	fill := &CTColor{Gradient: gradient}
	face := buildTextFace(family, models.TextObject{
		CtText: models.CtText{
			Size:   3,
			Weight: 700,
		},
	}, fill)
	if face.Fill.Gradient != gradient {
		t.Fatal("expected the text face to use the gradient fill")
	}
	if face.Style.Weight() != canvas.FontBold {
		t.Fatalf("expected bold face, got %v", face.Style)
	}
}

func TestBuildTextFaceDefaultsToBlack(t *testing.T) {
	family := canvas.NewFontFamily("test")
	if err := family.LoadSystemFont("DejaVu Sans", canvas.FontRegular); err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	face := buildTextFace(family, models.TextObject{
		CtText: models.CtText{
			Size: 3,
		},
	}, nil)
	if face.Fill.Color.A == 0 {
		t.Fatalf("expected default text fill, got %v", face.Fill.Color)
	}
	if face.Fill.Color != canvas.Black {
		t.Fatalf("expected black default text fill, got %v", face.Fill.Color)
	}
}

func TestTextHScaleDefaultsToOne(t *testing.T) {
	if got := textHScale(models.TextObject{}); got != 1 {
		t.Fatalf("expected default horizontal scale 1, got %v", got)
	}
	if got := textHScale(models.TextObject{CtText: models.CtText{HScale: 0.5}}); got != 0.5 {
		t.Fatalf("expected horizontal scale 0.5, got %v", got)
	}
}

func TestTextFillDisabled(t *testing.T) {
	if !textFillDisabled(models.TextObject{CtText: models.CtText{Fill: "false"}}) {
		t.Fatal("expected Fill=false to disable text fill")
	}
	if textFillDisabled(models.TextObject{}) {
		t.Fatal("expected missing Fill to keep text fill enabled")
	}
}

func TestOutlineTextWithStrokeOnly(t *testing.T) {
	t.Skip("stroke-only text")
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	family := canvas.NewFontFamily("outline")
	if err := family.LoadFont(data, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	document := &Document{}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 50, Height: 20}},
		Font:          1,
		Size:          8,
		Fill:          "false",
		Stroke:        true,
		StrokeColor:   &models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}},
		TextCode:      []models.TextCode{{Value: "O", X: 1, Y: 10}},
	}}
	c := canvas.New(50, 20)
	ctx := canvas.NewContext(c)
	document.fonts = &Fonts{Fonts: map[models.StRefID]*canvas.FontFamily{1: family}}
	document.Text(ctx, object, nil, models.StBox{Width: 50, Height: 20})
	image := rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)
	if countNonWhite(image) == 0 {
		t.Fatal("stroke-only text was not rendered")
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
	glyphs := textCodeGlyphs(runes, []models.CTCGTransform{{
		CodePosition: 0,
		CodeCount:    int(^uint(0) >> 1),
		Glyphs:       []int{65},
	}}, 0)

	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %+v, want one mapped glyph", glyphs)
	}
}
