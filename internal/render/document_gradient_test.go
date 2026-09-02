package render

import (
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

func TestGraphicUnitVisibleDefaultsToTrue(t *testing.T) {
	if !(models.CTGraphicUnit{}).VisibleValue() {
		t.Fatal("expected an unspecified Visible attribute to be visible")
	}

	hidden := models.OptionalBool{}
	hidden.Set(false)
	if (models.CTGraphicUnit{Visible: hidden}).VisibleValue() {
		t.Fatal("expected Visible=false to hide the graphic unit")
	}

	shown := models.OptionalBool{}
	shown.Set(true)
	if !(models.CTGraphicUnit{Visible: shown}).VisibleValue() {
		t.Fatal("expected Visible=true to show the graphic unit")
	}
}

func TestAnnotationVisibleDefaultsToTrue(t *testing.T) {
	if !annotationVisible(&models.Annot{}) {
		t.Fatal("expected an unspecified Visible attribute to be visible")
	}

	hidden := models.OptionalBool{}
	hidden.Set(false)
	if annotationVisible(&models.Annot{Visible: hidden}) {
		t.Fatal("expected Visible=false to hide the annotation")
	}

	shown := models.OptionalBool{}
	shown.Set(true)
	if !annotationVisible(&models.Annot{Visible: shown}) {
		t.Fatal("expected Visible=true to show the annotation")
	}
}

func TestOFDGradientStopsDefaultToEvenEndpoints(t *testing.T) {
	var stops []models.Segment
	for _, value := range []color.RGBA{{R: 255, A: 255}, {B: 255, A: 255}} {
		stops = append(stops, models.Segment{Color: models.CTColor{Value: &models.Color{RGBA: value}}})
	}

	var gradient canvas.Grad
	addOFDGradientStops(&gradient, stops)
	if len(gradient) != 2 || gradient[0].Offset != 0 || gradient[1].Offset != 1 {
		t.Fatalf("unexpected default stops: %+v", gradient)
	}
}

func TestOFDLinearGradientMapModes(t *testing.T) {
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		MapUnit:    10,
		Segment: []models.Segment{
			{Position: 0, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}

	shd.MapType = "Repeat"
	repeat := newOFDLinearGradient(shd, func(point models.StPos) canvas.Point {
		return canvas.Point{X: point.X, Y: point.Y}
	})
	if got := repeat.At(20, 0); got.R != 255 || got.B != 0 {
		t.Fatalf("expected repeat to restart at first stop, got %v", got)
	}

	shd.MapType = "Reflect"
	reflect := newOFDLinearGradient(shd, func(point models.StPos) canvas.Point {
		return canvas.Point{X: point.X, Y: point.Y}
	})
	got := reflect.At(17.5, 0)
	if got.R <= got.B {
		t.Fatalf("expected reflect to move back toward the first stop, got %v", got)
	}
}

func TestOFDLinearGradientUsesCanvasGradientForPDF(t *testing.T) {
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		Segment: []models.Segment{
			{Position: 0, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}

	if _, ok := newOFDLinearGradient(shd, func(point models.StPos) canvas.Point {
		return canvas.Point{X: point.X, Y: point.Y}
	}).(*canvas.LinearGradient); !ok {
		t.Fatal("expected ordinary OFD gradient to use canvas.LinearGradient")
	}
}

func TestOFDLinearGradientExtend(t *testing.T) {
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		Extend:     3,
		Segment: []models.Segment{
			{Position: 0, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}
	gradient := newOFDLinearGradient(shd, func(point models.StPos) canvas.Point {
		return canvas.Point{X: point.X, Y: point.Y}
	})
	if got := gradient.At(-5, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("expected start extension, got %v", got)
	}
	if got := gradient.At(15, 0); got != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("expected end extension, got %v", got)
	}
}
