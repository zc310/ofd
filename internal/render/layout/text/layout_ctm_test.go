package text

import (
	"math"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

// 字框和自然步进分别从对象空间映射，不借 Y 缩放估计 X 宽度，也不重复缩放步进。
func TestBuildScalesGlyphWidthsAndAdvancesOnce(t *testing.T) {
	face := runPositionFace{}
	width := face.TextWidth("A")
	for _, tc := range []struct {
		name   string
		ctm    spec.CTM
		hScale float64
		deltas spec.StArray[float64]
		steps  []float64
	}{
		{"nonuniform", spec.CTM{2, 0, 0, 1, 0, 0}, 1, nil, []float64{width * 2, width * 2}},
		{"uniform", spec.CTM{2, 0, 0, 2, 0, 0}, 1, nil, []float64{width * 2, width * 2}},
		{"hscale", spec.CTM{3, 0, 0, 2, 0, 0}, .5, nil, []float64{width * 1.5, width * 1.5}},
		{"explicit", spec.CTM{2, 0, 0, 3, 7, 8}, 1, spec.StArray[float64]{4, 5}, []float64{8, 10}},
		{"short_delta", spec.CTM{2, 0, 0, 3, 0, 0}, 1, spec.StArray[float64]{4}, []float64{8, width * 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := spec.TextObject{CtText: spec.CtText{
				CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{X: 10, Y: 20, Width: 40, Height: 10}, CTM: &tc.ctm},
				Size:          1, HScale: tc.hScale,
			}}
			layout := Build(face, object, spec.TextCode{Value: "AAA", X: 1, Y: 4, DeltaX: tc.deltas}, 0)
			for i, glyph := range layout.Glyphs {
				wantWidth := width * tc.hScale * tc.ctm[0]
				if math.Abs(glyph.Width-wantWidth) > 1e-9 || math.Abs(glyph.Height-tc.ctm[3]) > 1e-9 {
					t.Errorf("字符%d尺寸=(%g,%g)，期望=(%g,%g)", i, glyph.Width, glyph.Height, wantWidth, tc.ctm[3])
				}
				if i > 0 && math.Abs(glyph.X-layout.Glyphs[i-1].X-tc.steps[i-1]) > 1e-9 {
					t.Errorf("字符%d步进=%g，期望=%g", i, glyph.X-layout.Glyphs[i-1].X, tc.steps[i-1])
				}
			}
			wantX, wantY := tc.ctm.Transform(1, 4)
			if layout.X != 10+wantX || math.Abs(layout.Y-(20+wantY-.8*tc.ctm[3])) > 1e-9 {
				t.Errorf("文字起点=(%g,%g)，期望=(%g,%g)", layout.X, layout.Y, 10+wantX, 20+wantY-.8*tc.ctm[3])
			}
		})
	}
}
