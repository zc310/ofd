package render

import (
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

type clippedPathBenchmarkContext struct{ DrawContext }

func (clippedPathBenchmarkContext) DrawPath(float64, float64, *geom.Path) {}

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
			object := models.PathObject{CtPath: models.CtPath{Fill: true, Stroke: models.NewOptionalBool(false)}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				(&Document{}).drawClippedPath(ctx, path, tc.clip, object)
			}
		})
	}
}
