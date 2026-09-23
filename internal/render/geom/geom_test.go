package geom

import (
	"image/color"
	"math"
	"testing"
)

// TestRectangleBounds 验证矩形路径的包围盒。
func TestRectangleBounds(t *testing.T) {
	r := Rectangle(10, 5).Bounds()
	if !Equal(r.X0, 0) || !Equal(r.Y0, 0) || !Equal(r.X1, 10) || !Equal(r.Y1, 5) {
		t.Fatalf("矩形包围盒 = %v", r)
	}
}

// TestPathTranslateBounds 验证平移后的包围盒。
func TestPathTranslateBounds(t *testing.T) {
	r := Rectangle(10, 5).Translate(3, 4).Bounds()
	if !Equal(r.X0, 3) || !Equal(r.Y0, 4) || !Equal(r.X1, 13) || !Equal(r.Y1, 9) {
		t.Fatalf("平移后包围盒 = %v", r)
	}
}

// TestPathAndOr 验证布尔交/并运算的包围盒，确认移植的路径引擎可用。
func TestPathAndOr(t *testing.T) {
	a := Rectangle(10, 10)
	b := Rectangle(10, 10).Translate(5, 5)

	inter := a.And(b).Bounds()
	if !Equal(inter.X0, 5) || !Equal(inter.Y0, 5) || !Equal(inter.X1, 10) || !Equal(inter.Y1, 10) {
		t.Fatalf("交集包围盒 = %v", inter)
	}

	union := a.Or(b).Bounds()
	if !Equal(union.X0, 0) || !Equal(union.Y0, 0) || !Equal(union.X1, 15) || !Equal(union.Y1, 15) {
		t.Fatalf("并集包围盒 = %v", union)
	}
}

// TestPathStrokeBounds 验证描边展开后的包围盒宽度符合线宽。
func TestPathStrokeBounds(t *testing.T) {
	p := &Path{}
	p.MoveTo(0, 0)
	p.LineTo(10, 0)
	s := p.Stroke(2, ButtCapper{}, RoundJoiner{}, 0.01)
	r := s.Bounds()
	if math.Abs(r.H()-2) > 0.1 {
		t.Fatalf("描边高度 = %g，期望约 2", r.H())
	}
	if r.X0 < -0.01 || r.X1 > 10.01 {
		t.Fatalf("描边横向范围异常 = %v", r)
	}
}

// TestPathSettle 验证 Settle 可整理填充区域而不报错。
func TestPathSettle(t *testing.T) {
	p := Rectangle(10, 10)
	p2 := Rectangle(4, 4).Translate(3, 3)
	combined := p.Append(p2).Settle(EvenOdd)
	if combined.Empty() {
		t.Fatal("Settle 结果为空")
	}
}

// TestGradAt 验证渐变色标插值与端点钳制。
func TestGradAt(t *testing.T) {
	g := Grad{}
	g.Add(0, color.RGBA{0, 0, 0, 255})
	g.Add(1, color.RGBA{255, 255, 255, 255})
	mid := g.At(0.5)
	if mid.R == 0 || mid.R == 255 {
		t.Fatalf("中点颜色 = %v", mid)
	}
	if got := g.At(2); got.R != 255 {
		t.Fatalf("高端点 = %v", got)
	}
	if got := g.At(-1); got.R != 0 {
		t.Fatalf("低端点 = %v", got)
	}
}

// TestLinearGradientAt 验证线性渐变采样。
func TestLinearGradientAt(t *testing.T) {
	g := Grad{}
	g.Add(0, color.RGBA{0, 0, 0, 255})
	g.Add(1, color.RGBA{255, 0, 0, 255})
	lg := g.ToLinear(Point{0, 0}, Point{10, 0})
	if c := lg.At(0, 0); c.R != 0 {
		t.Fatalf("起点颜色 = %v", c)
	}
	if c := lg.At(10, 0); c.R != 255 {
		t.Fatalf("终点颜色 = %v", c)
	}
	if c := lg.At(5, 0); c.R == 0 || c.R == 255 {
		t.Fatalf("中点颜色 = %v", c)
	}
}
