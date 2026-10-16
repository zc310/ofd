package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

type gradientRunEngine struct {
	fakeFontEngine
	fill *CTColor
}

func (e *gradientRunEngine) FaceObject(_ FontFamily, _ models.TextObject, fill *CTColor) FontFace {
	e.fill = fill
	return metricsFontFace{}
}

func TestNativeTextGradientUsesBoundaryCoordinatesAcrossRuns(t *testing.T) {
	stops := geom.Grad{{Offset: 0, Color: color.RGBA{R: 240, A: 255}}, {Offset: 1, Color: color.RGBA{B: 240, A: 255}}}
	linear := geom.NewLinearGradient(geom.Point{X: -17, Y: 58}, geom.Point{X: 162, Y: -14})
	linear.Grad, linear.Extend = stops, [2]bool{false, true}
	radial := geom.NewRadialGradient(geom.Point{X: 15, Y: 10}, 3, geom.Point{X: 20, Y: 10}, 20)
	radial.Grad, radial.Extend = stops, [2]bool{true, false}
	for _, test := range []struct {
		name     string
		ctm      models.CTM
		hScale   float64
		gradient geom.Gradient
	}{
		{"identity_axial", models.CTM{1, 0, 0, 1, 0, 0}, 1, linear},
		{"intro_title_axial", models.CTM{9.8778, 0, 0, 9.8778, 0, 0}, 1, linear},
		{"rotated_axial", models.CTM{0, 2, -2, 0, 3, 4}, 1, linear},
		{"sheared_axial", models.CTM{2, 0.5, 1, 3, 4, -2}, 0.7, linear},
		{"scaled_radial", models.CTM{2, 0, 0, 2, 3, 4}, 1, radial},
		{"rotated_radial", models.CTM{0, 2, -2, 0, 3, 4}, 1, radial},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &gradientRunEngine{}
			object := models.TextObject{CtText: models.CtText{CTGraphicUnit: models.CTGraphicUnit{CTM: &test.ctm}, HScale: test.hScale}}
			fill := &CTColor{Gradient: test.gradient}
			faces := newTextFaces(engine, metricsFontFamily{}, object, fill, metricsFontFace{}, test.hScale)
			faces.native = true
			for _, origin := range []geom.Point{{X: 0, Y: 0.836}, {X: 7, Y: 3}} {
				engine.fill = nil
				faces.run(origin.X, origin.Y)
				if engine.fill == nil {
					t.Fatal("原生渐变未按 run 重建字体面")
				}
				mapped := engine.fill.Gradient
				switch gradient := mapped.(type) {
				case *geom.LinearGradient:
					if gradient.Extend != linear.Extend {
						t.Fatal("轴向渐变丢失 Extend")
					}
				case *geom.RadialGradient:
					if gradient.Extend != radial.Extend {
						t.Fatal("径向渐变丢失 Extend")
					}
				default:
					t.Fatalf("渐变未保留原生类型: %T", mapped)
				}
				for _, point := range []geom.Point{{}, {X: 2, Y: 0.5}, {X: 9, Y: -2}, {X: -8, Y: 13}} {
					x, y := test.ctm.Transform(origin.X+point.X*test.hScale, origin.Y-point.Y)
					want := test.gradient.At(x, y)
					got := mapped.At(point.X, point.Y)
					if colorDistance(got, want) > 1 {
						t.Errorf("run %v 点 %v 的颜色 = %v，Boundary 坐标 (%g,%g) 应为 %v", origin, point, got, x, y, want)
					}
				}
			}
			if fill.Gradient != test.gradient || linear.Start != (geom.Point{X: -17, Y: 58}) || radial.R0 != 3 {
				t.Fatal("原始渐变被修改")
			}
		})
	}
}

func TestNativeRadialTextGradientRejectsEllipticalAndInvalidTransforms(t *testing.T) {
	gradient := geom.NewRadialGradient(geom.Point{}, 0, geom.Point{}, 20)
	for _, matrix := range []geom.Matrix{
		geom.Identity.Scale(2, 1),
		{{1, 0.5, 0}, {0, 1, 0}},
		geom.Identity.Scale(0, 1),
		geom.Identity.Scale(math.Inf(1), 1),
	} {
		if mapped := nativeTextGradient(gradient, matrix); mapped != nil {
			t.Errorf("矩阵 %v 被错误地作为圆形径向渐变处理", matrix)
		}
	}
	for _, gradient := range []*geom.RadialGradient{
		geom.NewRadialGradient(geom.Point{}, 0, geom.Point{X: math.MaxFloat64}, 20),
		geom.NewRadialGradient(geom.Point{}, 0, geom.Point{}, math.MaxFloat64),
	} {
		if mapped := nativeTextGradient(gradient, geom.Identity.Scale(0.5, 0.5)); mapped != nil {
			t.Error("径向渐变换算溢出后仍被交给原生后端")
		}
	}
}

func colorDistance(a, b color.RGBA) int {
	distance := 0
	for index, value := range []uint8{a.R, a.G, a.B, a.A} {
		delta := int(value) - int([]uint8{b.R, b.G, b.B, b.A}[index])
		if delta < 0 {
			delta = -delta
		}
		distance = max(distance, delta)
	}
	return distance
}
