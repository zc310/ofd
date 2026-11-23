package render

import (
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
)

type clippedPathBenchmarkContext struct{ DrawContext }

func (clippedPathBenchmarkContext) DrawPath(float64, float64, *geom.Path) {}

type nativeClippedPathBenchmarkContext struct{ clippedPathBenchmarkContext }

func (nativeClippedPathBenchmarkContext) DrawClippedPath(*geom.Path, *geom.Path) bool {
	return true
}

func BenchmarkDrawNativeClippedPath(b *testing.B) {
	path := geom.Circle(30).Translate(50, 50)
	clip := geom.Rectangle(50, 100)
	for _, stroke := range []bool{false, true} {
		b.Run(map[bool]string{false: "fill", true: "dashed-stroke"}[stroke], func(b *testing.B) {
			ctx := nativeClippedPathBenchmarkContext{
				clippedPathBenchmarkContext{newCanvasBackend(canvas.NewContext(canvas.New(100, 100)))},
			}
			ctx.SetFillColor(canvas.Black)
			ctx.SetStrokeColor(canvas.Red)
			ctx.SetStrokeWidth(1)
			pattern := spec.StArray[float64]{2, 1}
			object := spec.PathObject{}
			object.Fill, object.DashPattern = !stroke, &pattern
			object.Stroke.Set(stroke)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				(&Document{}).drawClippedPath(ctx, path, clip, object)
			}
		})
	}
}

func BenchmarkDrawClippedPath(b *testing.B) {
	path := geom.Circle(30).Translate(50, 50)
	for _, tc := range []struct {
		name string
		clip *geom.Path
	}{
		{"contained", geom.Rectangle(100, 100)},
		{"partial", geom.Rectangle(50, 100)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			ctx := clippedPathBenchmarkContext{newCanvasBackend(canvas.NewContext(canvas.New(100, 100)))}
			ctx.SetFillColor(canvas.Black)
			object := spec.PathObject{CtPath: spec.CtPath{Fill: true, Stroke: spec.NewBool(false)}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				(&Document{}).drawClippedPath(ctx, path, tc.clip, object)
			}
		})
	}
}
