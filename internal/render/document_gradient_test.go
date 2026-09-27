package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
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

func TestRadialMapUnitDefaultsToRadiusSpan(t *testing.T) {
	shd := &models.CTRadialShd{
		StartPoint:  models.StPos{X: 0, Y: 0},
		EndPoint:    models.StPos{X: 0, Y: 0},
		StartRadius: 10,
		EndRadius:   70,
		MapType:     "Repeat",
		Segment: []models.Segment{
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, G: 255, A: 255}}}},
			{Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
		},
	}
	gradient := newOFDRadialGradient(shd, func(point models.StPos) geom.Point {
		return geom.Point{X: point.X, Y: point.Y}
	}, nil)
	// 半径 70 是第一周期终点（蓝），半径 80 进入下一周期，应回到偏黄而不是停在蓝。
	outer := gradient.At(80, 0)
	if outer.B > outer.R {
		t.Fatalf("repeat beyond end radius = %v, want a new cycle toward yellow", outer)
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
	// 只有 Extend=3（两侧夹取端点色）与原生 *geom.LinearGradient 语义一致，
	// 才能保留 PDF 矢量着色；其余 Extend 值必须按位采样。
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		Extend:     3,
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

// Direct 轴向渐变的 Extend 必须按位生效（GB/T 33190 表 29）：
// 0 两侧都不延伸（轴外透明）、1 只向起点延长、2 只向终点延长、3 两侧都延长。
// 之前 Direct 一律走原生 *geom.LinearGradient，轴外全部夹取端点色，
// 等价于 Extend=3，导致 axial-extend.ofd 的四个矩形看起来完全一样。
func TestOFDLinearGradientDirectExtendBits(t *testing.T) {
	startColor := color.RGBA{R: 255, A: 255}
	endColor := color.RGBA{B: 255, A: 255}
	transparent := color.RGBA{}
	cases := []struct {
		extend                int
		beforeStart, afterEnd color.RGBA
		wantNative            bool
	}{
		{0, transparent, transparent, false},
		{1, startColor, transparent, false},
		{2, transparent, endColor, false},
		{3, startColor, endColor, true},
	}
	for _, tc := range cases {
		shd := &models.CTAxialShd{
			StartPoint: models.StPos{X: 0, Y: 0},
			EndPoint:   models.StPos{X: 10, Y: 0},
			Extend:     tc.extend,
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: startColor}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: endColor}}},
			},
		}
		gradient := newOFDLinearGradient(shd, func(point models.StPos) geom.Point {
			return geom.Point{X: point.X, Y: point.Y}
		}, nil)
		if _, native := gradient.(*geom.LinearGradient); native != tc.wantNative {
			t.Fatalf("Extend=%d 是否原生矢量渐变 = %v，期望 %v", tc.extend, native, tc.wantNative)
		}
		if got := gradient.At(-5, 0); got != tc.beforeStart {
			t.Fatalf("Extend=%d 起点之前取色 = %v，期望 %v", tc.extend, got, tc.beforeStart)
		}
		if got := gradient.At(15, 0); got != tc.afterEnd {
			t.Fatalf("Extend=%d 终点之后取色 = %v，期望 %v", tc.extend, got, tc.afterEnd)
		}
	}
}

// 两圆插值族的参数 t 应与 pixman/PDF 一致：R0=10、R1=50 时，
// 半径 30 处 t=0.5，圆心处 t=-0.25（起点之前），半径 60 处 t=1.25（终点之后）。
// 轴外的根只有在 Extend 对应位打开时才可用，否则该点无解（ok=false）。
func TestRadialParameterMatchesPixman(t *testing.T) {
	g := geom.NewRadialGradient(geom.Point{}, 10, geom.Point{}, 50)
	cases := []struct {
		distance float64
		extend   int
		want     float64
		ok       bool
	}{
		{30, 0, 0.5, true},
		{10, 0, 0, true},
		{50, 0, 1, true},
		// 圆心（t=-0.25）只有在起点延伸时可用。
		{0, 0, 0, false},
		{0, 1, -0.25, true},
		{0, 3, -0.25, true},
		// 半径 60（t=1.25）只有在终点延伸时可用。
		{60, 0, 0, false},
		{60, 2, 1.25, true},
		{60, 3, 1.25, true},
	}
	for _, tc := range cases {
		tGot, ok := g.RadialParameter(tc.distance, 0, tc.extend)
		if ok != tc.ok || (ok && math.Abs(tGot-tc.want) > 1e-9) {
			t.Fatalf("距离 %.1f Extend=%d 的 RadialParameter = (%v, %v)，期望 (%v, %v)", tc.distance, tc.extend, tGot, ok, tc.want, tc.ok)
		}
	}
}

// Direct 径向渐变的 Extend 必须按位生效（GB/T 33190 表 29）：
// 0 轴外透明、1 只向起点延长、2 只向终点延长、3 两侧都延长。
func TestOFDRadialGradientDirectExtendBits(t *testing.T) {
	startColor := color.RGBA{R: 255, A: 255}
	endColor := color.RGBA{B: 255, A: 255}
	transparent := color.RGBA{}
	cases := []struct {
		extend                int
		beforeStart, afterEnd color.RGBA
		wantNative            bool
	}{
		{0, transparent, transparent, false},
		{1, startColor, transparent, false},
		{2, transparent, endColor, false},
		{3, startColor, endColor, true},
	}
	for _, tc := range cases {
		shd := &models.CTRadialShd{
			StartPoint:  models.StPos{X: 0, Y: 0},
			StartRadius: 10,
			EndPoint:    models.StPos{X: 0, Y: 0},
			EndRadius:   50,
			Extend:      tc.extend,
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: startColor}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: endColor}}},
			},
		}
		gradient := newOFDRadialGradient(shd, func(point models.StPos) geom.Point {
			return geom.Point{X: point.X, Y: point.Y}
		}, nil)
		if _, native := gradient.(*geom.RadialGradient); native != tc.wantNative {
			t.Fatalf("Extend=%d 是否原生矢量渐变 = %v，期望 %v", tc.extend, native, tc.wantNative)
		}
		// 圆心在起点圆内（t=-0.25），半径 60 在终点圆外（t=1.25）。
		if got := gradient.At(0, 0); got != tc.beforeStart {
			t.Fatalf("Extend=%d 起点圆内取色 = %v，期望 %v", tc.extend, got, tc.beforeStart)
		}
		if got := gradient.At(60, 0); got != tc.afterEnd {
			t.Fatalf("Extend=%d 终点圆外取色 = %v，期望 %v", tc.extend, got, tc.afterEnd)
		}
		// 轴线内始终是渐变，不受 Extend 影响。
		if got := gradient.At(30, 0); got.R == 0 || got.B == 0 || got.R >= 255 || got.B >= 255 {
			t.Fatalf("Extend=%d 轴线内取色 = %v，期望红蓝之间的插值色", tc.extend, got)
		}
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
			// 该测试关注 CTM 坐标处理，用 Extend=3 走原生矢量渐变。
			Extend: 3,
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

// 起始圆退化为焦点的偏心径向：焦点背面的点没有半径非负的解，属于未着色
// 区域。Extend 默认 0 时该区域必须保持透明，不能铺上起点色。GB/T 33190
// 表 29 与 PDF Type 3 径向着色在此一致（pdftoppm 参考渲染同为空）。
func TestEccentricRadialFocusBacksideStaysTransparent(t *testing.T) {
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
	// 焦点在 x=16，终点圆左缘在 x=20。x=4 在圆外，应透明而不是起点绿色。
	x := int(math.Round(4.0 / 25.4 * 72.0))
	y := img.Bounds().Dy() - int(math.Round(10.0/25.4*72.0))
	got := img.RGBAAt(x, y)
	if got.A != 0 {
		t.Fatalf("焦点背面 = %v，期望透明（A=0）", got)
	}
	// 终点圆附近的着色区域仍应可见。
	x = int(math.Round(24.0 / 25.4 * 72.0))
	got = img.RGBAAt(x, y)
	if got.A == 0 {
		t.Fatalf("终点圆附近 = %v，期望非透明", got)
	}
}

// 实色填充 + 需要栅格化的渐变描边：描边必须画在填充之上。此前先画描边图片、
// 后画实色填充，填充会盖住描边内侧；再叠加 Extend=0 的轴外透明，左右描边会
// 整条消失（shading.ofd 第 6 页“纯色+渐变描边”）。
func TestSolidFillWithRasterStrokeDrawsStrokeOnTop(t *testing.T) {
	object := models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 0, Y: 0, Width: 30, Height: 20}, LineWidth: 2},
		Fill:          true,
		FillColor:     &models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 200, G: 255, B: 200, A: 255}}},
		StrokeColor: &models.CTColor{AxialShd: &models.CTAxialShd{
			StartPoint: models.StPos{X: 0, Y: 10},
			EndPoint:   models.StPos{X: 30, Y: 10},
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
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
	object.Stroke.Set(true)

	const dpi = 200.0
	c := canvas.New(30, 20)
	ctx := canvas.NewContext(c)
	var document Document
	document.Path(newCanvasBackend(ctx), object, nil, models.StBox{Width: 30, Height: 20})
	img := rasterizer.Draw(c, canvas.DPI(dpi), canvas.DefaultColorSpace)
	pick := func(xmm, ymm float64) color.RGBA {
		x := int(math.Round(xmm / 25.4 * dpi))
		y := img.Bounds().Dy() - int(math.Round(ymm/25.4*dpi))
		return img.RGBAAt(x, y)
	}
	// 左边描边轴内侧（x=0.5mm）应为起点红色，而不是被填充盖成绿色。
	if left := pick(0.5, 10); left.R < 150 || left.G > 120 {
		t.Fatalf("左边描边内侧 = %v，期望起点红色（描边被填充覆盖）", left)
	}
	// 右边描边轴内侧（x=29.5mm）应为终点蓝色。
	if right := pick(29.5, 10); right.B < 150 || right.R > 120 {
		t.Fatalf("右边描边内侧 = %v，期望终点蓝色（描边被填充覆盖）", right)
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

// 文字填充的轴向渐变坐标与路径图元一致：位于图元 Boundary 内的毫米坐标系，
// 由绘制流程按 TextCode 定位平移。text-directions.ofd 第 3 页的"轴向渐变 A→B"
// 曾把 StartPoint/EndPoint 写成 0..1 单位坐标，导致渐变只有 1mm 长，文字只采样到
// 末端色，看起来完全没有渐变。
func TestTextAxialGradientUsesBoundaryMillimeterCoordinates(t *testing.T) {
	// 与 text-directions.json 一致：0..78mm 横跨实际文字宽度。
	// 该测试关注坐标空间，用 Extend=3 走原生矢量渐变。
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 9},
		EndPoint:   models.StPos{X: 78, Y: 9},
		Extend:     3,
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 220, A: 255}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 220, A: 255}}}},
		},
	}
	fill := &models.CTColor{AxialShd: shd}

	var document Document
	got := document.updateCtColor(fill)
	if got == nil || got.Gradient == nil {
		t.Fatalf("文字填充渐变 = %v，期望非 nil", got)
	}
	linear, ok := got.Gradient.(*geom.LinearGradient)
	if !ok {
		t.Fatalf("文字填充渐变类型 = %T，期望 *geom.LinearGradient", got.Gradient)
	}
	// 渐变坐标留在 Boundary 局部毫米空间：绘制时由 TextCode 定位统一平移，
	// 起点 (0,9)、终点 (78,9)，不能再叠加 Boundary 偏移，否则会平移两次。
	wantStart := geom.Point{X: 0, Y: 9}
	wantEnd := geom.Point{X: 78, Y: 9}
	if linear.Start != wantStart || linear.End != wantEnd {
		t.Fatalf("文字渐变轴 = %v..%v，期望 %v..%v", linear.Start, linear.End, wantStart, wantEnd)
	}
	// 局部空间下 78mm 的渐变必须覆盖 0..78mm，文字才能看到完整色阶：
	// 四分之一处仍偏红，四分之三处已偏蓝。
	if early := linear.At(19.5, 9); early.R <= early.B {
		t.Fatalf("渐变 1/4 处 = %v，期望红分量大于蓝分量", early)
	}
	if late := linear.At(58.5, 9); late.B <= late.R {
		t.Fatalf("渐变 3/4 处 = %v，期望蓝分量大于红分量", late)
	}
}

func TestOFDMeshGradientClampsToEdgeOutsideControlPoints(t *testing.T) {
	// 控制点只是插值锚点，不限制着色范围：落在控制点之外的采样点应延续边缘颜色。
	gradient := newOFDGouraudGradient(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, EdgeFlag: 1, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, EdgeFlag: 2, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, EdgeFlag: 2, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
			{X: 10, Y: 10, EdgeFlag: 2, Color: basicMeshColor(color.RGBA{R: 255, B: 255, A: 255})},
		},
	}, identityGradientTransform, nil)
	if gradient == nil {
		t.Fatal("网格渐变 = nil，期望非 nil")
	}
	inside := gradient.At(5, 5)
	if inside.A == 0 {
		t.Fatalf("控制点之内取色 = %v，期望不透明", inside)
	}
	// 未声明着色区域时，区域外必须保持透明（Extend 未开启）。
	if outside := gradient.At(5, 40); outside.A != 0 {
		t.Fatalf("未声明着色区域时控制点之外取色 = %v，期望透明", outside)
	}

	// 声明比控制点更大的着色区域 (0,0)-(10,20)：渐变点未覆盖整个 Boundary，
	// 区域内未覆盖的部分必须按边缘颜色延续。
	area := geom.RectFromSize(0, 0, 10, 20)
	shaded := newOFDGouraudGradientArea(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, EdgeFlag: 1, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, EdgeFlag: 2, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, EdgeFlag: 2, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
			{X: 10, Y: 10, EdgeFlag: 2, Color: basicMeshColor(color.RGBA{R: 255, B: 255, A: 255})},
		},
	}, identityGradientTransform, &area, nil)
	// 区域内、控制点之下（y=15 超出控制点范围 y<=10）：夹回 y=10 后应取到
	// 底边颜色，而不是透明。
	below := shaded.At(5, 15)
	if below.A == 0 {
		t.Fatalf("着色区域下边界取色 = %v，期望延续边缘颜色而不是透明", below)
	}
	if below.B <= below.G {
		t.Fatalf("着色区域下边界取色 = %v，期望贴近底边颜色（蓝分量不小于绿分量）", below)
	}
	// 区域之外仍保持透明：Extend 只影响着色区域之外，不改变区域内的夹取。
	if far := shaded.At(500, 500); far.A != 0 {
		t.Fatalf("着色区域之外取色 = %v，期望透明（Extend 未开启）", far)
	}
	// 左右同样按边缘延续。
	if side := shaded.At(0, 5); side.A == 0 {
		t.Fatalf("着色区域左边界取色 = %v，期望不透明", side)
	}
}

func TestMeshGradientTextFillsGlyphOverflowingControlPoints(t *testing.T) {
	// text-directions.json 的 ID=54：基线位于 Boundary 底边，渐变控制点只覆盖
	// Boundary 上部 30mm，字形下部溢出控制点范围。网格渐变必须和同参数纯色填充
	// 一样填满整个字形，不能把下半截留成透明。
	mesh := &creator.Color{Gouraud: &creator.GouraudShading{
		Points: []creator.GouraudPoint{
			{X: 0, Y: 0, Color: creator.Color{R: 255, G: 100, B: 50}},
			{X: 170, Y: 0, EdgeFlag: 1, Color: creator.Color{R: 50, G: 200, B: 100}},
			{X: 0, Y: 30, EdgeFlag: 2, Color: creator.Color{R: 50, G: 100, B: 255}},
			{X: 170, Y: 30, EdgeFlag: 2, Color: creator.Color{R: 200, G: 50, B: 200}},
		},
	}}
	solid := &creator.Color{R: 30, G: 30, B: 30}
	codeY := float64Ptr(35)

	meshHeight := textInkHeightMM(t, renderCreatorPage(t, creator.Text{
		X: 20, Y: 235, Width: 170, Height: 35, Font: "楷体", Size: 22, Weight: 700,
		Fill: boolPtrT(), FillColor: mesh,
		TextCodes: []creator.TextCode{{Y: codeY, Value: "网格渐变"}},
	}))
	solidHeight := textInkHeightMM(t, renderCreatorPage(t, creator.Text{
		X: 20, Y: 235, Width: 170, Height: 35, Font: "楷体", Size: 22, Weight: 700,
		Fill: boolPtrT(), FillColor: solid,
		TextCodes: []creator.TextCode{{Y: codeY, Value: "网格渐变"}},
	}))

	if meshHeight <= 0 || solidHeight <= 0 {
		t.Fatalf("墨迹高度 网格=%.1fmm 纯色=%.1fmm，期望都大于 0", meshHeight, solidHeight)
	}
	if diff := meshHeight - solidHeight; diff > 0.5 || diff < -0.5 {
		t.Fatalf("网格渐变墨迹高 %.1fmm，纯色 %.1fmm，差 %+.1fmm，期望一致（字形下部未被丢弃）",
			meshHeight, solidHeight, diff)
	}
}

// textInkHeightMM 返回渲染图中有墨迹的行跨度（毫米）。
func textInkHeightMM(t *testing.T, img image.Image) float64 {
	t.Helper()
	bounds := img.Bounds()
	scale := float64(bounds.Dx()) / 210.0
	top, bottom := -1, -1
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		painted := 0
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 246 || g>>8 < 246 || b>>8 < 246 {
				painted++
			}
		}
		if painted > 3 {
			if top < 0 {
				top = y
			}
			bottom = y
		}
	}
	if top < 0 {
		return 0
	}
	return float64(bottom-top) / scale
}

func boolPtrT() *bool { value := true; return &value }

// 文字渐变必须按图元 Boundary 空间取样。
//
// 后端渲染文字时会把每个像素映回该 run 的字体空间（y 向上、原点在 run 基线
// 起点）后再取渐变，因此直接使用 Boundary 空间的渐变会让 y 轴方向反转、
// 非零 TextCode 偏移错位，并使渐变按每个 run 重新开始。水平渐变恰好对 y 方向
// 不敏感、且 TextCode.X 为 0 时原点重合，所以轴向渐变文字看起来正常属于巧合。
func TestTextGradientUsesBoundarySpace(t *testing.T) {
	img := renderCreatorPage(t, creator.Text{
		X: 20, Y: 100, Width: 100, Height: 40, Font: "楷体", Size: 30,
		Fill: boolPtrT(), FillColor: blueToRedVertical(40),
		TextCodes: []creator.TextCode{{Value: "水"}},
	})

	edges := textInkEdgesMM(t, img)
	if edges.height <= 0 {
		t.Fatal("未渲染出文字墨迹")
	}
	// 渐变自上而下 蓝(0,0,255) → 红(255,0,0)，Boundary 空间 y 向下，
	// 因此字形下部必须比上部更红：(R-B) 自上而下递增。
	top := redMinusBlue(t, img, edges.top, edges.top+edges.height/3)
	bottom := redMinusBlue(t, img, edges.bottom-edges.height/3, edges.bottom)
	if bottom <= top {
		t.Fatalf("竖向渐变自上而下 (R-B) 顶部 %.3f、底部 %.3f，期望底部更大（渐变 y 轴被反转或未按 Boundary 空间取样）",
			top, bottom)
	}
	if bottom-top < 0.3 {
		t.Fatalf("竖向渐变自上而下 (R-B) 仅从 %.3f 变到 %.3f，期望有明确渐变跨度", top, bottom)
	}
}

// 同一文字对象的多个 TextCode 共享一条渐变，渐变必须跨 run 连续，不能在每个
// run 起点重新开始。
func TestTextGradientContinuousAcrossTextCodes(t *testing.T) {
	img := renderCreatorPage(t, creator.Text{
		X: 20, Y: 100, Width: 100, Height: 30, Font: "楷体", Size: 14,
		Fill: boolPtrT(), FillColor: blueToRedHorizontal(100),
		TextCodes: []creator.TextCode{
			{Value: "渐变填充", X: float64Ptr(0), Y: float64Ptr(30)},
			{Value: "填充渐变", X: float64Ptr(55), Y: float64Ptr(30)},
		},
	})

	// 文字对象位于页面 x=20..120：第一个 TextCode 占局部 0..约 40mm，
	// 第二个从局部 55mm 开始，因此页面 x≈78 处渐变位置应约 0.58。
	first := gradientPositionAt(t, img, 22, 38)
	second := gradientPositionAt(t, img, 77, 85)
	if second <= first {
		t.Fatalf("第二个 TextCode 处渐变位置 %.3f 未超过第一个 TextCode 的 %.3f，期望渐变跨 run 连续而非在 run 起点重启",
			second, first)
	}
	if second < 0.4 {
		t.Fatalf("第二个 TextCode（局部 x≈58mm，渐变 0..100mm）处渐变位置仅 %.3f，期望约 0.6；渐变在 run 起点重启", second)
	}
	if first > 0.3 {
		t.Fatalf("第一个 TextCode（局部 x≈8mm）处渐变位置 %.3f 偏大，起点附近应接近 0", first)
	}
}

func blueToRedVertical(height float64) *creator.Color {
	return &creator.Color{Axial: &creator.AxialShading{
		StartPoint: "10 0", EndPoint: fmt.Sprintf("10 %g", height),
		Segments: []creator.ColorStop{
			{Position: 0, Color: creator.Color{R: 0, G: 0, B: 255}},
			{Position: 1, Color: creator.Color{R: 255, G: 0, B: 0}},
		},
	}}
}

func blueToRedHorizontal(width float64) *creator.Color {
	return &creator.Color{Axial: &creator.AxialShading{
		StartPoint: "0 15", EndPoint: fmt.Sprintf("%g 15", width),
		Segments: []creator.ColorStop{
			{Position: 0, Color: creator.Color{R: 0, G: 0, B: 255}},
			{Position: 1, Color: creator.Color{R: 255, G: 0, B: 0}},
		},
	}}
}

// redMinusBlue 返回指定纵向区间内墨迹像素的平均 (R-B)/255。
func redMinusBlue(t *testing.T, img image.Image, topMM, bottomMM float64) float64 {
	t.Helper()
	bounds := img.Bounds()
	scale := float64(bounds.Dx()) / 210.0
	sum, count := 0, 0
	for y := int(topMM * scale); y < int(bottomMM*scale) && y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 >= 250 && g>>8 >= 250 && b>>8 >= 250 {
				continue
			}
			sum += int(r>>8) - int(b>>8)
			count++
		}
	}
	if count == 0 {
		t.Fatalf("纵向区间 %.1f..%.1fmm 内没有墨迹像素", topMM, bottomMM)
	}
	return float64(sum) / float64(count) / 255
}

// gradientPositionAt 返回指定横向区间内墨迹像素的平均渐变位置，蓝→红渐变
// 满足 (R-B)/255 = 2t-1。
func gradientPositionAt(t *testing.T, img image.Image, leftMM, rightMM float64) float64 {
	t.Helper()
	bounds := img.Bounds()
	scale := float64(bounds.Dx()) / 210.0
	sum, count := 0, 0
	for x := int(leftMM * scale); x < int(rightMM*scale) && x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 >= 250 && g>>8 >= 250 && b>>8 >= 250 {
				continue
			}
			sum += int(r>>8) - int(b>>8)
			count++
		}
	}
	if count == 0 {
		t.Fatalf("横向区间 %.1f..%.1fmm 内没有墨迹像素", leftMM, rightMM)
	}
	return (float64(sum)/float64(count)/255 + 1) / 2
}
