package geom

import (
	"math"
	"testing"
)

// TestRadialParameterExtendCoversNoExtend 锁定 Extend 的包含关系：
// 向终点延长得到的解集必须包含不延伸的解集，且在不延伸已经给出解的地方
// 取到完全相同的 t。延伸只负责扫描范围之外的点，不能改写扫描自身的颜色。
func TestRadialParameterExtendCoversNoExtend(t *testing.T) {
	grads := []struct {
		name string
		g    *RadialGradient
	}{
		{"两圆外离", NewRadialGradient(Point{18, 35}, 8, Point{52, 35}, 18)},
		{"两圆内含", NewRadialGradient(Point{18, 35}, 6, Point{25, 35}, 14)},
		{"两圆相交", NewRadialGradient(Point{12, 35}, 6, Point{24, 35}, 12)},
		{"同心增长", NewRadialGradient(Point{0, 0}, 10, Point{0, 0}, 20)},
		{"焦点族", NewRadialGradient(Point{0, 0}, 0, Point{30, 0}, 10)},
	}
	for _, tc := range grads {
		t.Run(tc.name, func(t *testing.T) {
			var base, grew int
			for y := -40.0; y <= 80.0; y += 0.25 {
				for x := -40.0; x <= 80.0; x += 0.25 {
					t0, ok0 := tc.g.RadialParameter(x, y, 0)
					t2, ok2 := tc.g.RadialParameter(x, y, 2)
					if ok0 {
						base++
						if !ok2 {
							t.Fatalf("不延伸有解 t=%v，但向终点延长无解于 (%v, %v)", t0, x, y)
						}
						if math.Abs(t0-t2) > 1e-9 {
							t.Fatalf("(%v, %v) 不延伸 t=%v，向终点延长 t=%v；延伸不得改写扫描内的取值", x, y, t0, t2)
						}
					}
					if ok2 && !ok0 {
						grew++
					}
					// 向起点延长同理。
					t1, ok1 := tc.g.RadialParameter(x, y, 1)
					if ok0 {
						if !ok1 || math.Abs(t0-t1) > 1e-9 {
							t.Fatalf("(%v, %v) 不延伸 t=%v，向起点延长 t=%v(%v)；延伸不得改写扫描内的取值", x, y, t0, t1, ok1)
						}
					}
				}
			}
			if grew == 0 {
				t.Errorf("向终点延长未新增任何解，无法验证包含关系")
			}
			t.Logf("不延伸有解 %d 点，其中向终点延长额外新增 %d 点", base, grew)
		})
	}
}
