package render

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

type resolutionTestContext struct {
	DrawContext
	resolution geom.Resolution
}

func (c resolutionTestContext) RasterResolution() geom.Resolution { return c.resolution }

func TestGradientOffscreenResolutionTracksDeviceAndTransform(t *testing.T) {
	doc := &Document{dpi: geom.DPI(96)}
	for _, tc := range []struct {
		name              string
		dpi, sx, sy, want float64
	}{
		{"preview", 96, 1, 1, 96},
		{"print", 600, 1, 1, 600},
		{"scaled container", 96, 2, 3, 288},
		{"bounded", 600, 3, 3, 1200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clipTestSurface(t, func(ctx DrawContext) {
				ctx.Scale(tc.sx, tc.sy)
				got := doc.paintResolution(resolutionTestContext{ctx, geom.DPI(tc.dpi)}).DPI()
				if math.Abs(got-tc.want) > 1e-8 {
					t.Fatalf("DPI=%g，期望%g", got, tc.want)
				}
			})
		})
	}
	previous := newOffscreenSurfaceFactory
	t.Cleanup(func() { newOffscreenSurfaceFactory = previous })
	var requested geom.Resolution
	newOffscreenSurfaceFactory = func(_, _ float64, resolution geom.Resolution) OffscreenSurface {
		requested = resolution
		return nil
	}
	clipTestSurface(t, func(ctx DrawContext) {
		var budget renderBudget
		budget.reset()
		doc.drawMeshPaintGradient(resolutionTestContext{ctx, geom.DPI(96)}, geom.Rectangle(10, 10),
			geom.NewLinearGradient(geom.Point{}, geom.Point{X: 10}), models.StBox{Width: 40, Height: 40}, nil, &budget)
	})
	if math.Abs(requested.DPI()-96) > 1e-8 {
		t.Fatalf("离屏实际请求DPI=%g，期望96", requested.DPI())
	}
}
