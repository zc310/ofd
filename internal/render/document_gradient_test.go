package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
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

	var gradient geom.Grad
	addOFDGradientStops(&gradient, stops, nil)
	if len(gradient) != 2 || gradient[0].Offset != 0 || gradient[1].Offset != 1 {
		t.Fatalf("unexpected default stops: %+v", gradient)
	}
}

func TestOFDGradientStopsFillOmittedEndpoints(t *testing.T) {
	stops := []models.Segment{
		{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
		{Position: 0.5, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}},
	}
	var gradient geom.Grad
	addOFDGradientStops(&gradient, stops, nil)
	if len(gradient) != 3 || gradient[0].Offset != 0 || gradient[1].Offset != 0.5 || gradient[2].Offset != 1 {
		t.Fatalf("unexpected partial stops: %+v", gradient)
	}
	if gradient[0].Color.R != 255 || gradient[1].Color.B != 255 || gradient[2].Color.G != 255 {
		t.Fatalf("unexpected stop colors: %+v", gradient)
	}
}

func TestOFDLinearGradientMapModes(t *testing.T) {
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		MapUnit:    10,
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}

	shd.MapType = "Repeat"
	repeat := newOFDLinearGradient(shd, func(point models.StPos) geom.Point {
		return geom.Point{X: point.X, Y: point.Y}
	}, nil)
	if got := repeat.At(20, 0); got.R != 255 || got.B != 0 {
		t.Fatalf("expected repeat to restart at first stop, got %v", got)
	}

	shd.MapType = "Reflect"
	reflect := newOFDLinearGradient(shd, func(point models.StPos) geom.Point {
		return geom.Point{X: point.X, Y: point.Y}
	}, nil)
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
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}

	if _, ok := newOFDLinearGradient(shd, func(point models.StPos) geom.Point {
		return geom.Point{X: point.X, Y: point.Y}
	}, nil).(*geom.LinearGradient); !ok {
		t.Fatal("expected ordinary OFD gradient to use geom.LinearGradient")
	}
}

func TestTranslatedGradientPreservesPageCoordinates(t *testing.T) {
	gradient := newOFDLinearGradient(&models.CTAxialShd{
		StartPoint: models.StPos{X: 10, Y: 20},
		EndPoint:   models.StPos{X: 20, Y: 20},
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}, identityGradientTransform, nil)
	local := translateGradient(gradient, 10, 20)
	if got, want := local.At(5, 0), gradient.At(15, 20); got != want {
		t.Fatalf("translated gradient = %v, want %v", got, want)
	}
}

func TestOFDLinearGradientExtend(t *testing.T) {
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		Extend:     3,
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}
	gradient := newOFDLinearGradient(shd, func(point models.StPos) geom.Point {
		return geom.Point{X: point.X, Y: point.Y}
	}, nil)
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
	}, identityGradientTransform, nil)

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
	}, identityGradientTransform, nil).(*ofdMeshGradient)
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
	}, identityGradientTransform, nil).(*ofdMeshGradient)
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
	}, identityGradientTransform, nil)
	if got := gradient.At(2, 2); got != (color.RGBA{G: 255, A: 255}) {
		t.Fatalf("outside color = %v, want back color", got)
	}
}

func TestMeshGradientSpatialIndexMatchesBruteForce(t *testing.T) {
	// 空间索引必须与逐三角形线性查找给出相同的命中结果，避免为提速而漏掉三角形。
	triangles := buildTestMeshTriangles(40)
	indexed := &ofdMeshGradient{triangles: triangles}
	seed := uint32(12345)
	next := func() float64 {
		seed = seed*1664525 + 1013904223
		return float64(seed>>8) / float64(1<<24)
	}
	checked := 0
	for i := 0; i < 20000; i++ {
		x := next()*44 - 2
		y := next()*44 - 2
		want, wantOK := sampleMeshTriangles(triangles, geom.Point{X: x, Y: y})
		got, gotOK := indexed.sampleAt(geom.Point{X: x, Y: y})
		if wantOK != gotOK {
			t.Fatalf("at (%g,%g): brute ok=%v, indexed ok=%v", x, y, wantOK, gotOK)
		}
		if wantOK && want != got {
			t.Fatalf("at (%g,%g): brute=%v, indexed=%v", x, y, want, got)
		}
		if wantOK {
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no sample point hit the mesh")
	}
}

func BenchmarkMeshGradientAt(b *testing.B) {
	triangles := buildTestMeshTriangles(100)
	gradient := &ofdMeshGradient{triangles: triangles}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x := float64(i%10000) / 10000 * 100
		gradient.At(x, x)
	}
}

func BenchmarkMeshGradientAtLinear(b *testing.B) {
	triangles := buildTestMeshTriangles(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x := float64(i%10000) / 10000 * 100
		sampleMeshTriangles(triangles, geom.Point{X: x, Y: x})
	}
}

func buildTestMeshTriangles(cells int) []ofdMeshTriangle {
	point := func(x, y int) geom.Point { return geom.Point{X: float64(x), Y: float64(y)} }
	colorAt := func(x, y int) color.RGBA {
		return color.RGBA{R: uint8(x * 3), G: uint8(y * 3), B: 40, A: 255}
	}
	triangles := make([]ofdMeshTriangle, 0, cells*cells*2)
	for x := 0; x < cells; x++ {
		for y := 0; y < cells; y++ {
			a, b, c, d := point(x, y), point(x+1, y), point(x, y+1), point(x+1, y+1)
			ca, cb, cc, cd := colorAt(x, y), colorAt(x+1, y), colorAt(x, y+1), colorAt(x+1, y+1)
			triangles = append(triangles,
				ofdMeshTriangle{p0: a, p1: b, p2: d, c0: ca, c1: cb, c2: cd},
				ofdMeshTriangle{p0: a, p1: d, p2: c, c0: ca, c1: cd, c2: cc})
		}
	}
	return triangles
}

func TestOFDColorAlphaUsesOpacitySemantics(t *testing.T) {
	value := models.Color{RGBA: color.RGBA{R: 255, A: 255}}
	alpha := uint8(128)
	got := ofdColorRGBA(models.CTColor{Value: &value, Alpha: &alpha})
	n := color.NRGBAModel.Convert(got).(color.NRGBA)
	if n.R != 255 || n.A < 127 || n.A > 128 {
		t.Fatalf("color = %v, want straight red with approximately 128 alpha", n)
	}
}

func TestPathGradientAppliesObjectAlphaToFillAndStroke(t *testing.T) {
	alpha := uint8(51)
	gradientColor := func() *models.CTColor {
		return &models.CTColor{AxialShd: &models.CTAxialShd{
			StartPoint: models.StPos{X: 0, Y: 0},
			EndPoint:   models.StPos{X: 10, Y: 0},
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
			},
		}}
	}
	object := &models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{Alpha: &alpha},
		Fill:          true,
		FillColor:     gradientColor(),
		StrokeColor:   gradientColor(),
	}}
	ctx := canvas.NewContext(canvas.New(20, 20))
	var document Document

	document.updatePathGradients(newCanvasBackend(ctx), object, nil, 20)

	if got := ctx.Style.Fill.Gradient.At(0, 20); got.A != 51 {
		t.Fatalf("fill gradient alpha = %d, want 51", got.A)
	}
	if got := ctx.Style.Stroke.Gradient.At(0, 20); got.A != 51 {
		t.Fatalf("stroke gradient alpha = %d, want 51", got.A)
	}
}

func TestUpdatePathGradientsReturnsUnscaledGradientForMeshReuse(t *testing.T) {
	alpha := uint8(51)
	gradientColor := func() *models.CTColor {
		return &models.CTColor{AxialShd: &models.CTAxialShd{
			StartPoint: models.StPos{X: 0, Y: 0},
			EndPoint:   models.StPos{X: 10, Y: 0},
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
			},
		}}
	}
	object := &models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{Alpha: &alpha},
		Fill:          true,
		FillColor:     gradientColor(),
		StrokeColor:   gradientColor(),
	}}
	ctx := canvas.NewContext(canvas.New(20, 20))
	var document Document

	fillGradient, strokeGradient := document.updatePathGradients(newCanvasBackend(ctx), object, nil, 20)

	if got := ctx.Style.Fill.Gradient.At(0, 20); got.A != 51 {
		t.Fatalf("ctx fill gradient alpha = %d, want 51", got.A)
	}
	if got := fillGradient.At(0, 20); got.A != 255 {
		t.Fatalf("returned fill gradient alpha = %d, want unscaled 255", got.A)
	}
	if got := strokeGradient.At(0, 20); got.A != 255 {
		t.Fatalf("returned stroke gradient alpha = %d, want unscaled 255", got.A)
	}
}

func TestPathGradientCoordinatesIgnoreObjectCTM(t *testing.T) {
	// OFD 渐变坐标位于对象 CTM 已经生效的坐标系中，再乘一次 CTM 会让渐变
	// 整体偏移到图形之外（intro.ofd 第 9 页时间轴渐变会因此只剩端点颜色）。
	ctm := models.CTM{0.5, 0, 0, 0.5, 100, 50}
	object := &models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 10, Y: 20, Width: 40, Height: 10},
			CTM:      &ctm,
		},
		Fill: true,
		FillColor: &models.CTColor{AxialShd: &models.CTAxialShd{
			StartPoint: models.StPos{X: 0, Y: 0},
			EndPoint:   models.StPos{X: 20, Y: 0},
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
			},
		}},
	}}
	ctx := canvas.NewContext(canvas.New(100, 100))
	var document Document

	document.updatePathGradients(newCanvasBackend(ctx), object, nil, 100)

	gradient, ok := ctx.Style.Fill.Gradient.(*canvas.LinearGradient)
	if !ok {
		t.Fatalf("fill gradient type = %T, want *canvas.LinearGradient", ctx.Style.Fill.Gradient)
	}
	// 仅叠加 Boundary 偏移 (10, 20) 并翻转 Y 轴：起点 (10, 80)、终点 (30, 80)。
	// 若错误地再次叠加 CTM，起点会变成 (110, 30)。
	if gradient.Start != (canvas.Point{X: 10, Y: 80}) || gradient.End != (canvas.Point{X: 30, Y: 80}) {
		t.Fatalf("gradient axis = %v..%v, want (10,80)..(30,80)", gradient.Start, gradient.End)
	}
}

func TestRepeatReflectGradientRendersInPDF(t *testing.T) {
	for _, mapType := range []string{"Repeat", "Reflect"} {
		object := models.PathObject{CtPath: models.CtPath{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 0, Y: 0, Width: 30, Height: 10}},
			Fill:          true,
			FillColor: &models.CTColor{AxialShd: &models.CTAxialShd{
				StartPoint: models.StPos{X: 0, Y: 0},
				EndPoint:   models.StPos{X: 10, Y: 0},
				MapType:    mapType,
				MapUnit:    10,
				Segment: []models.Segment{
					{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
					{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
				},
			}},
			AbbreviatedData: models.SVGPath{
				{Type: models.MoveTo, Points: []models.StPos{{X: 0, Y: 0}}},
				{Type: models.LineTo, Points: []models.StPos{{X: 30, Y: 0}}},
				{Type: models.LineTo, Points: []models.StPos{{X: 30, Y: 10}}},
				{Type: models.LineTo, Points: []models.StPos{{X: 0, Y: 10}}},
				{Type: models.Close},
			},
		}}
		c := canvas.New(30, 20)
		ctx := canvas.NewContext(c)
		var document Document
		document.Path(newCanvasBackend(ctx), object, nil, models.StBox{Width: 30, Height: 20})
		img := rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)
		// 画布 Y 向上。矩形中心在 (15, 15)。
		px := int(math.Round(15.0 / 25.4 * 72.0))
		mid := img.RGBAAt(px, img.Bounds().Dy()-px)
		if mid.R > 250 && mid.G > 250 && mid.B > 250 {
			t.Fatalf("%s gradient is blank at mid pixel %v", mapType, mid)
		}
	}
}

func TestEccentricRadialKeepsStartColorOutsideFocus(t *testing.T) {
	object := models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 0, Y: 0, Width: 30, Height: 20}},
		Fill:          true,
		FillColor: &models.CTColor{RadialShd: &models.CTRadialShd{
			StartPoint:  models.StPos{X: 16, Y: 10},
			EndPoint:    models.StPos{X: 26, Y: 10},
			StartRadius: 0,
			EndRadius:   6,
			Segment: []models.Segment{
				{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 200, A: 255}}}},
				{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, G: 255, A: 255}}}},
			},
		}},
		AbbreviatedData: models.SVGPath{
			{Type: models.MoveTo, Points: []models.StPos{{X: 0, Y: 0}}},
			{Type: models.LineTo, Points: []models.StPos{{X: 30, Y: 0}}},
			{Type: models.LineTo, Points: []models.StPos{{X: 30, Y: 20}}},
			{Type: models.LineTo, Points: []models.StPos{{X: 0, Y: 20}}},
			{Type: models.Close},
		},
	}}
	c := canvas.New(30, 20)
	ctx := canvas.NewContext(c)
	var document Document
	document.Path(newCanvasBackend(ctx), object, nil, models.StBox{Width: 30, Height: 20})
	img := rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)
	// 焦点在 x=16，终点圆左缘在 x=20。x=4 在圆外，应保持起点绿色。
	x := int(math.Round(4.0 / 25.4 * 72.0))
	y := img.Bounds().Dy() - int(math.Round(10.0/25.4*72.0))
	got := img.RGBAAt(x, y)
	if got.G < 150 || got.R > 40 {
		t.Fatalf("left of focus = %v, want start green", got)
	}
}

func TestGraphicOpacityUsesOpacitySemantics(t *testing.T) {
	if got := graphicOpacity(nil); got != 255 {
		t.Fatalf("nil alpha = %d, want 255", got)
	}
	if got := graphicOpacity(uint8ptr(0)); got != 0 {
		t.Fatalf("zero alpha = %d, want 0", got)
	}
	if got := graphicOpacity(uint8ptr(51)); got != 51 {
		t.Fatalf("alpha 51 = %d, want 51", got)
	}
	if got := graphicOpacity(uint8ptr(255)); got != 255 {
		t.Fatalf("full alpha = %d, want 255", got)
	}
}

func uint8ptr(value uint8) *uint8 {
	return &value
}

func basicMeshColor(value color.RGBA) models.CTColor {
	return models.CTColor{Value: &models.Color{RGBA: value}}
}
