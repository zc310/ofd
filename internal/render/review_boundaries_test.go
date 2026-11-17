package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/pkg/spec"
)

func TestPatternRejectsUnrepresentableTileIndexesBeforeChargingBudget(t *testing.T) {
	pattern := &spec.CtPattern{Width: math.SmallestNonzeroFloat64, Height: math.SmallestNonzeroFloat64,
		CellContent: spec.CellContent{CTPageBlock: spec.CTPageBlock{Items: []spec.PageItem{{Kind: spec.PageItemPath}}}},
	}
	path := geom.Rectangle(1e290, 1e290).Translate(1e300, 1e300)
	var budget renderBudget
	budget.reset()
	doc := &Document{}
	if doc.drawPatternPath(nil, path, pattern, spec.PathObject{}, spec.StBox{Width: 100, Height: 100}, nil, &budget) || budget.Usage().PatternTiles != 0 {
		t.Fatalf("浮点索引溢出被误当作有效图块：tiles=%d", budget.Usage().PatternTiles)
	}
}

func TestZeroAlphaGradientEndpointsDoNotPaintColor(t *testing.T) {
	var stops geom.Grad
	stops.Add(0, color.RGBA{R: 255})
	stops.Add(1, color.RGBA{B: 255, A: 255})
	for _, name := range []string{"native", "sampled"} {
		t.Run(name, func(t *testing.T) {
			var gradient geom.Gradient = stops.ToLinear(geom.Point{X: 10}, geom.Point{X: 20})
			if name == "sampled" {
				alpha := uint8(0)
				gradient = paint.NewLinearGradient(&spec.CTAxialShd{
					StartPoint: spec.StPos{X: 10}, EndPoint: spec.StPos{X: 20}, Extend: 1,
					Segment: []spec.Segment{
						{Color: spec.CTColor{Value: &spec.ColorValue{RGBA: color.RGBA{R: 255, A: 255}}, Alpha: &alpha}},
						{Color: spec.CTColor{Value: &spec.ColorValue{RGBA: color.RGBA{B: 255, A: 255}}}},
					},
				}, identityGradientTransform, nil)
			}
			ctx := newOffscreenSurface(30, 10, geom.DPMM(4))
			ctx.SetFillColor(geom.Black)
			ctx.ClearStroke()
			ctx.DrawPath(0, 0, geom.Rectangle(30, 10))
			ctx.SetFillGradient(gradient)
			ctx.DrawPath(0, 0, geom.Rectangle(30, 10))
			if got := ctx.Raster().RGBAAt(20, 20); got != geom.Black {
				t.Fatalf("透明红端点改变了黑色背景：%v", got)
			}
			t.Run("PDF", func(t *testing.T) {
				object := clipTestPath(t, `<PathObject Boundary="0 0 30 10" Fill="true" Stroke="false"><AbbreviatedData>M 0 0 L 30 0 L 30 10 L 0 10 C</AbbreviatedData></PathObject>`)
				alpha := uint8(0)
				extend := 3
				if name == "sampled" {
					extend = 1
				}
				object.FillColor = &spec.CTColor{AxialShd: &spec.CTAxialShd{StartPoint: spec.StPos{X: 10}, EndPoint: spec.StPos{X: 20}, Extend: extend, Segment: []spec.Segment{
					{Color: spec.CTColor{Value: &spec.ColorValue{RGBA: color.RGBA{R: 255, A: 255}}, Alpha: &alpha}},
					{Color: spec.CTColor{Value: &spec.ColorValue{RGBA: color.RGBA{B: 255, A: 255}}}},
				}}}
				surface, err := NewVectorSurface(30, 10, func(ctx DrawContext) {
					ctx.SetFillColor(geom.Black)
					ctx.ClearStroke()
					ctx.DrawPath(0, 0, geom.Rectangle(30, 10))
					doc := &Document{}
					doc.Path(ctx, object, nil, spec.StBox{Width: 30, Height: 10})
				})
				if err != nil {
					t.Fatal(err)
				}
				img := clipPDFImage(t, surface, 254)
				if got := color.RGBAModel.Convert(img.At(50, 50)).(color.RGBA); got != geom.Black {
					t.Fatalf("PDF透明端点改变背景：%v", got)
				}
			})
		})
	}
}
