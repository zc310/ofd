package paint

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func basicMeshColor(value color.RGBA) models.CTColor {
	return models.CTColor{Value: &models.Color{RGBA: value}}
}

func TestOFDGouraudGradientUsesEdgeFlags(t *testing.T) {
	gradient := NewGouraudGradient(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
			{X: 10, Y: 10, EdgeFlag: 1, Color: basicMeshColor(color.RGBA{A: 255})},
		},
	}, identityGradientTransform, nil, nil).(*ofdMeshGradient)
	if len(gradient.triangles) != 2 {
		t.Fatalf("triangle count = %d, want 2", len(gradient.triangles))
	}
	if got := gradient.At(8, 8); got.A == 0 {
		t.Fatalf("edge-flag triangle was not rendered: %v", got)
	}
}

// TestOFDGouraudGradientEdgeFlagTopology 对照 GB/T 33190 图 40「方向标志的
// 控制作用」逐格验证三角形拓扑：EdgeFlag=0 产生两个互不相连的三角形，
// EdgeFlag=1 复用 V1-V2 边、EdgeFlag=2 复用 V0-V2 边，各与上一个三角形
// 拼成一个四边形。
func TestOFDGouraudGradientEdgeFlagTopology(t *testing.T) {
	red := basicMeshColor(color.RGBA{R: 255, A: 255})
	grn := basicMeshColor(color.RGBA{G: 255, A: 255})
	blu := basicMeshColor(color.RGBA{B: 255, A: 255})

	triangles := func(points ...models.GouraudPoint) int {
		return len(NewGouraudGradient(&models.CTGouraudShd{Point: points},
			identityGradientTransform, nil, nil).(*ofdMeshGradient).triangles)
	}

	// EdgeFlag=0：每三点一个三角形，两个三角形彼此独立。
	if got := triangles(
		models.GouraudPoint{X: 5, Y: 15, Color: red},
		models.GouraudPoint{X: 0, Y: 8, Color: grn},
		models.GouraudPoint{X: 10, Y: 8, Color: blu},
		models.GouraudPoint{X: 5, Y: 3, EdgeFlag: 0, Color: red},
		models.GouraudPoint{X: 0, Y: -4, Color: grn},
		models.GouraudPoint{X: 10, Y: -4, Color: blu},
	); got != 2 {
		t.Fatalf("EdgeFlag=0 triangle count = %d, want 2", got)
	}
	// EdgeFlag=1 与 2 都只新增一个三角形，与上一个三角形共享一条边。
	for _, flag := range []int{1, 2} {
		if got := triangles(
			models.GouraudPoint{X: 5, Y: 15, Color: red},
			models.GouraudPoint{X: 0, Y: 8, Color: grn},
			models.GouraudPoint{X: 10, Y: 8, Color: blu},
			models.GouraudPoint{X: 5, Y: 1, EdgeFlag: flag, Color: red},
		); got != 2 {
			t.Fatalf("EdgeFlag=%d triangle count = %d, want 2", flag, got)
		}
	}

	// 顶点拓扑：共享边决定第二个三角形的位置。
	// EdgeFlag=1 复用 V1-V2 边（此处为 (0,8)-(10,8) 水平边），新点落在其下方，
	// 四边形为 V0-V1-新点-V2，菱形内部（含上下两个三角形）都必须被填充。
	diamond := NewGouraudGradient(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 5, Y: 15, Color: red},
			{X: 0, Y: 8, Color: grn},
			{X: 10, Y: 8, Color: blu},
			{X: 5, Y: 1, EdgeFlag: 1, Color: red},
		},
	}, identityGradientTransform, nil, nil)
	for _, probe := range []struct {
		name   string
		x, y   float64
		inside bool
	}{
		{"上三角形内", 5, 10, true},
		{"下三角形内（仅 EdgeFlag=1 复用边才存在）", 5, 4, true},
		{"共享边之外", 5, 18, false},
		{"左侧之外", -5, 8, false},
	} {
		if got := diamond.At(probe.x, probe.y).A > 200; got != probe.inside {
			t.Fatalf("EdgeFlag=1 %s：覆盖=%v，期望 %v", probe.name, got, probe.inside)
		}
	}

	// EdgeFlag=2 复用 V0-V2 边（此处为 (5,15)-(10,8)），新点在右上，
	// 四边形为 V1-V0-新点-V2，右上区域（仅 EdgeFlag=2 复用边才存在）必须被填充。
	quad := NewGouraudGradient(&models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 5, Y: 15, Color: red},
			{X: 0, Y: 8, Color: grn},
			{X: 10, Y: 8, Color: blu},
			{X: 15, Y: 15, EdgeFlag: 2, Color: red},
		},
	}, identityGradientTransform, nil, nil)
	for _, probe := range []struct {
		name   string
		x, y   float64
		inside bool
	}{
		{"左三角形内", 3, 10, true},
		{"右三角形内（仅 EdgeFlag=2 复用边才存在）", 12, 12, true},
		{"下方之外", 8, 3, false},
		{"右侧之外", 20, 12, false},
	} {
		if got := quad.At(probe.x, probe.y).A > 200; got != probe.inside {
			t.Fatalf("EdgeFlag=2 %s：覆盖=%v，期望 %v", probe.name, got, probe.inside)
		}
	}
}

func TestOFDLaGouraudGradientBuildsLattice(t *testing.T) {
	gradient := NewLatticeGradient(&models.CTLaGouraudShd{
		VerticesPerRow: 2,
		Point: []models.LaGouraudPoint{
			{X: 0, Y: 0, Color: basicMeshColor(color.RGBA{R: 255, A: 255})},
			{X: 10, Y: 0, Color: basicMeshColor(color.RGBA{G: 255, A: 255})},
			{X: 0, Y: 10, Color: basicMeshColor(color.RGBA{B: 255, A: 255})},
			{X: 10, Y: 10, Color: basicMeshColor(color.RGBA{A: 255})},
		},
	}, identityGradientTransform, nil, nil).(*ofdMeshGradient)
	if len(gradient.triangles) != 2 {
		t.Fatalf("triangle count = %d, want 2", len(gradient.triangles))
	}
	if got := gradient.At(5, 5); got.A == 0 {
		t.Fatalf("lattice center was not rendered: %v", got)
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
