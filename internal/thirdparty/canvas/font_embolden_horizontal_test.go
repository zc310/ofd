package canvas

import "testing"

func TestEmboldenShrinksHoleWhoseStartRayOverlapsOuterStep(t *testing.T) {
	// 孔洞起点的水平射线与外轮廓的台阶重合；两端各贡献半次向下穿越，
	// 不能把整个台阶忽略，否则会将孔洞误判为外轮廓。
	path := MustParseSVGPath("M3 10L3 3L7 3L7 10z M0 12L15 12L15 10L10 10L10 0L0 0z")
	for _, original := range []*Path{path, path.Reverse(), path.Copy().Translate(48, 6)} {
		before := original.String()
		bounds := original.Bounds()
		for _, distance := range []float64{0.2, 0.4} {
			after := emboldenGlyphPath(original, distance)
			for _, shear := range []float64{0, 0.3} {
				matrix := Identity.Shear(shear, 0)
				got := after.Copy().Transform(matrix)
				for _, x := range []float64{3 + distance/2, 7 - distance/2} {
					point := matrix.Dot(Point{bounds.X0 + x, bounds.Y0 + 6})
					if !got.ContainsPoint(point.X, point.Y, NonZero) {
						t.Fatalf("加粗未收缩孔洞：distance=%g，shear=%g，point=%v", distance, shear, point)
					}
				}
				point := matrix.Dot(Point{bounds.X0 + 5, bounds.Y0 + 6})
				if got.ContainsPoint(point.X, point.Y, NonZero) {
					t.Fatal("加粗填满了孔洞中心")
				}
			}
		}
		if original.String() != before {
			t.Fatal("加粗修改了原始轮廓")
		}
	}
}
