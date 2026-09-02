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
