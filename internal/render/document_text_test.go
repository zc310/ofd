package render

import (
	"testing"

	"github.com/tdewolff/canvas"
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
