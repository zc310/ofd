package render

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

type sharedPaintTextCall struct {
	glyphMatrix geom.Matrix
	paintMatrix geom.Matrix
	paint       geom.Paint
	clip        *geom.Path
}

type sharedPaintTextCapture struct {
	DrawContext

	accept   bool
	calls    []sharedPaintTextCall
	fallback int
}

func (*sharedPaintTextCapture) ShadingText() bool { return true }

func (c *sharedPaintTextCapture) DrawSharedPaintTextLine(_ TextRun, clip *geom.Path, paint geom.Paint, paintMatrix geom.Matrix) bool {
	c.calls = append(c.calls, sharedPaintTextCall{c.CurrentMatrix(), paintMatrix, paint, clip})
	return c.accept
}

func (c *sharedPaintTextCapture) DrawClippedTextLine(TextRun, *geom.Path) bool {
	c.fallback++
	return true
}

func TestSharedTextPaintKeepsGlyphOffsetsAndBoundarySampling(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "共享画笔", false: "后端拒绝后回退"}[accepted], func(t *testing.T) {
			doc := clipTestDocument(t)
			ctm := models.CTM{2, .5, .3, 1.5, 1, 2}
			object := models.TextObject{CtText: models.CtText{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 5, Y: 5, Width: 30, Height: 20}, CTM: &ctm},
				Size:          4,
				HScale:        .7,
				TextCode:      []models.TextCode{{X: 1, Y: 5, DeltaX: models.StArray[float64]{1.7, 2.3}, DeltaY: models.StArray[float64]{.2, -.1}, Value: "ABC"}},
				FillColor: &models.CTColor{AxialShd: &models.CTAxialShd{
					StartPoint: models.StPos{}, EndPoint: models.StPos{X: 30, Y: 20}, Extend: 3,
					Segment: []models.Segment{
						{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
						{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
					},
				}},
			}}
			outer := geom.Identity.Translate(2, 3).Rotate(10)
			var capture *sharedPaintTextCapture
			clipTestSurface(t, func(ctx DrawContext) {
				ctx.Transform(outer)
				capture = &sharedPaintTextCapture{DrawContext: ctx, accept: accepted}
				doc.Text(capture, object, nil, models.StBox{Width: 40, Height: 40})
			})
			if len(capture.calls) != 3 {
				t.Fatalf("字形run数=%d，期望3", len(capture.calls))
			}
			wantFallback := 0
			if !accepted {
				wantFallback = 3
			}
			if capture.fallback != wantFallback {
				t.Fatalf("原生裁剪回退次数=%d，期望%d", capture.fallback, wantFallback)
			}
			origins := []geom.Point{{X: 1, Y: 5}, {X: 2.7, Y: 5.2}, {X: 5, Y: 5.1}}
			transform := textPageTransform(object, 40, nil)
			source := doc.textShadingGradient(object.FillColor, nil)
			for i, call := range capture.calls {
				if !call.glyphMatrix.Equals(outer.Mul(transform(origins[i].X, origins[i].Y))) {
					t.Fatalf("字形%d的精确定位被共享画笔改变", i)
				}
				if call.paintMatrix != capture.calls[0].paintMatrix || call.clip == nil {
					t.Fatal("各run没有共享固定画笔坐标或丢失局部裁剪")
				}
				point := geom.Point{X: 1, Y: .5}
				world := call.glyphMatrix.Dot(point)
				local := call.paintMatrix.Inv().Dot(world)
				got := call.paint.Gradient.At(local.X, local.Y)
				x, y := ctm.Transform(origins[i].X+point.X*object.HScale, origins[i].Y-point.Y)
				if colorDistance(got, source.At(x, y)) > 1 {
					t.Fatalf("字形%d的渐变取样偏离Boundary空间：%v", i, got)
				}
			}
		})
	}
}
