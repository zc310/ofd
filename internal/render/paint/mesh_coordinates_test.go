package paint

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func TestMeshShadingAreaFollowsControlPointCoordinateMapping(t *testing.T) {
	value := &spec.ColorValue{}
	value.RGBA = color.RGBA{R: 255, A: 255}
	red := spec.CTColor{Value: value}
	source := &spec.CTColor{GouraudShd: &spec.CTGouraudShd{Point: []spec.GouraudPoint{
		{X: 0, Y: 0, Color: red},
		{X: 10, Y: 0, Color: red},
		{X: 0, Y: 10, Color: red},
		{X: 10, Y: 10, EdgeFlag: 2, Color: red},
	}}}
	area := geom.RectFromSize(0, 0, 10, 20)
	for _, matrix := range []geom.Matrix{
		geom.Identity.Translate(50, 200),
		geom.Identity.Translate(50, 200).Scale(1, -1),
	} {
		gradient := (&Resolver{}).Gradient(source, func(p spec.StPos) geom.Point {
			return matrix.Dot(geom.Point{X: p.X, Y: p.Y})
		}, &area, nil)
		inside := matrix.Dot(geom.Point{X: 5, Y: 15})
		outside := matrix.Dot(geom.Point{X: 5, Y: 25})
		if gradient == nil || gradient.At(inside.X, inside.Y).A != 255 || gradient.At(outside.X, outside.Y).A != 0 {
			t.Fatalf("着色区域没有随控制点变换：%v", matrix)
		}
		if area != geom.RectFromSize(0, 0, 10, 20) {
			t.Fatal("修改了调用方的着色区域")
		}
	}
}
