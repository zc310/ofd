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

func TestOFDGouraudGradientInterpolatesTriangleColors(t *testing.T) {
	gradient := newOFDGouraudGradient(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
		},
	}, identityGradientTransform)

	if got := gradient.At(0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("vertex color = %v", got)
	}
	got := gradient.At(10.0/3, 10.0/3)
	if got.R < 80 || got.R > 90 || got.G < 80 || got.G > 90 || got.B < 80 || got.B > 90 {
		t.Fatalf("center interpolation = %v, want approximately equal channels", got)
	}
}

func TestOFDGouraudGradientUsesEdgeFlags(t *testing.T) {
	gradient := newOFDGouraudGradient(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
			{X: 10, Y: 10, EdgeFlag: 1, Color: basicMeshColor(color.RGBA{A: 255})},
		},
	}, identityGradientTransform).(*ofdMeshGradient)
	if len(gradient.triangles) != 2 {
		t.Fatalf("triangle count = %d, want 2", len(gradient.triangles))
	}
	if got := gradient.At(8, 8); got.A == 0 {
		t.Fatalf("edge-flag triangle was not rendered: %v", got)
	}
}

func TestOFDLaGouraudGradientBuildsLattice(t *testing.T) {
	gradient := newOFDLaGouraudGradient(&models.CTLaGouraudShd{
		VerticesPerRow: 2,
		Point: []models.LaGouraudPoint{
			{X: 0, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
			{X: 10, Y: 10, Color: basicMeshColor(color.RGBA{A: 255})},
		},
	}, identityGradientTransform).(*ofdMeshGradient)
	if len(gradient.triangles) != 2 {
		t.Fatalf("triangle count = %d, want 2", len(gradient.triangles))
	}
	if got := gradient.At(5, 5); got.A == 0 {
		t.Fatalf("lattice center was not rendered: %v", got)
	}
}

func TestOFDMeshGradientUsesBackColorOutsideMesh(t *testing.T) {
	gradient := newOFDGouraudGradient(&models.CTGouraudShd{
		Extend:    1,
		BackColor: &models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}},
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 1, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 0, Y: 1, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
		},
	}, identityGradientTransform)
	if got := gradient.At(2, 2); got != (color.RGBA{G: 255, A: 255}) {
		t.Fatalf("outside color = %v, want back color", got)
	}
}

func TestOFDColorAlphaUsesOpacitySemantics(t *testing.T) {
	value := models.Color{RGBA: color.RGBA{R: 255, A: 255}}
	alpha := uint8(128)
	got := ofdColorRGBA(models.CTColor{Value: &value, Alpha: &alpha})
	if got.R != 255 || got.A < 127 || got.A > 128 {
		t.Fatalf("color = %v, want straight red with approximately 128 alpha", got)
	}
}

func TestGraphicOpacityUsesTransparencySemantics(t *testing.T) {
	if got := graphicOpacity(nil); got != 255 {
		t.Fatalf("nil transparency = %d, want 255", got)
	}
	if got := graphicOpacity(uint8ptr(0)); got != 255 {
		t.Fatalf("zero transparency = %d, want 255", got)
	}
	if got := graphicOpacity(uint8ptr(51)); got != 204 {
		t.Fatalf("51 transparency = %d, want 204", got)
	}
	if got := graphicOpacity(uint8ptr(255)); got != 0 {
		t.Fatalf("full transparency = %d, want 0", got)
	}
}

func uint8ptr(value uint8) *uint8 {
	return &value
}

func basicMeshColor(value color.RGBA) models.CTColor {
	return models.CTColor{Value: &models.Color{RGBA: value}}
}
