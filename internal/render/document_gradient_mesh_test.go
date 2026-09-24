package render

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestOFDGouraudEdgeFlag3ReusesFirstEdge 回归：规范只定义 EdgeFlag 0/1/2，
// 个别文件沿用 PDF 语义使用 3（复用上一个三角形的 v0-v1 边）。此前
// newOFDGouraudGradient 直接丢弃该点导致三角形缺失，宽容处理后应正常覆盖。
func TestOFDGouraudEdgeFlag3ReusesFirstEdge(t *testing.T) {
	red := models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}
	green := models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	blue := models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}
	shd := &models.CTGouraudShd{
		Point: []models.GouraudPoint{
			{X: 0, Y: 0, Color: red},
			{X: 10, Y: 0, Color: green},
			{X: 0, Y: 10, Color: blue},
			// flag 3 → {v0, v1, 新顶点} = {(0,0),(10,0),(10,10)}。
			{X: 10, Y: 10, Color: red, EdgeFlag: 3},
		},
	}
	identity := func(p models.StPos) geom.Point { return geom.Point{X: p.X, Y: p.Y} }
	resolve := func(c models.CTColor) color.RGBA {
		if c.Value != nil {
			return c.Value.RGBA
		}
		return color.RGBA{A: 255}
	}
	gradient := newOFDGouraudGradient(shd, identity, resolve)
	if gradient == nil {
		t.Fatal("newOFDGouraudGradient returned nil")
	}
	// (9,8) 位于 flag 3 三角形内部、三角形 1 之外，旧实现会因丢弃该点而透明。
	got := gradient.At(9, 8)
	if got.A == 0 {
		t.Fatal("EdgeFlag=3 三角形未被覆盖（点被丢弃）")
	}
	// 三角形 1 内部的点仍应被覆盖。
	first := gradient.At(1, 1)
	if first.A == 0 {
		t.Fatal("首个三角形未被覆盖")
	}
	// 两个三角形之外应保持透明（未开启 extend）。
	if outside := gradient.At(50, 50); outside.A != 0 {
		t.Fatalf("网格外点应透明, got %v", outside)
	}
}

// TestOFDLaGouraudRejectsPartialTailRow 回归：规则网格的 Point 数量不是
// VerticesPerRow 的整数倍时，此前 newOFDLaGouraudGradient 用整除静默丢弃
// 不完整的尾行（数据损坏时产出看似正常实则缺失的网格）。现在应整体拒绝。
func TestOFDLaGouraudRejectsPartialTailRow(t *testing.T) {
	red := models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}
	identity := func(p models.StPos) geom.Point { return geom.Point{X: p.X, Y: p.Y} }
	resolve := func(c models.CTColor) color.RGBA {
		if c.Value != nil {
			return c.Value.RGBA
		}
		return color.RGBA{A: 255}
	}
	// 行优先 2×3：行 0 为 y=0，行 1 为 y=10，每行 x=0/10/20。
	lp := func(x, y float64) models.LaGouraudPoint {
		return models.LaGouraudPoint{X: x, Y: y, Color: red}
	}
	valid := &models.CTLaGouraudShd{
		VerticesPerRow: 3,
		Point: []models.LaGouraudPoint{
			lp(0, 0), lp(10, 0), lp(20, 0),
			lp(0, 10), lp(10, 10), lp(20, 10),
		},
	}
	if gradient := newOFDLaGouraudGradient(valid, identity, resolve); gradient == nil {
		t.Fatal("完整 2×3 网格应生成渐变")
	} else if got := gradient.At(8, 6); got.A == 0 {
		t.Fatal("网格内部未被覆盖")
	}
	// 追加第 7 个点（残行）：应整体拒绝，而不是只渲染前两行。
	broken := &models.CTLaGouraudShd{
		VerticesPerRow: 3,
		Point:          append(append([]models.LaGouraudPoint{}, valid.Point...), lp(30, 10)),
	}
	if gradient := newOFDLaGouraudGradient(broken, identity, resolve); gradient != nil {
		t.Fatal("含尾行的规则网格应整体拒绝，而不是静默丢弃尾行")
	}
}
