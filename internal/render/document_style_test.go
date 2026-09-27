package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestPathStrokeScalesWithObjectCTM 回归：线宽、虚线、斜接限制定义在对象坐标系，
// 必须按对象 CTM 缩放。此前带 pt→mm（0.3528）之类缩放 CTM 的描边会过粗。
func TestPathStrokeScalesWithObjectCTM(t *testing.T) {
	object := models.PathObject{CtPath: models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth: 1.021,
			CTM:       &models.CTM{0.3528, 0, 0, 0.3528, 0, 0},
		},
	}}
	scale := pathStrokeScale(object, nil)
	if math.Abs(scale-0.3528) > 1e-9 {
		t.Fatalf("pathStrokeScale = %g, want 0.3528", scale)
	}

	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.updateCtPathStyle(newCanvasBackend(ctx), &object.CtPath, nil, scale)
	want := 1.021 * 0.3528
	if math.Abs(ctx.Style.StrokeWidth-want) > 1e-9 {
		t.Fatalf("stroke width = %g, want %g", ctx.Style.StrokeWidth, want)
	}

	// 无 CTM 时缩放系数为 1，保持原有线宽。
	if scale := pathStrokeScale(models.PathObject{}, nil); scale != 1 {
		t.Fatalf("pathStrokeScale without CTM = %g, want 1", scale)
	}
}

func TestApplyFillDefaultsToTransparent(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	ctx.SetFillColor(color.RGBA{R: 255, A: 255})
	var document Document
	document.applyFill(newCanvasBackend(ctx), nil, nil)
	if ctx.Style.Fill.Color != geom.Transparent {
		t.Fatalf("expected transparent default fill, got %v", ctx.Style.Fill.Color)
	}
}

func TestApplyFillKeepsExplicitColor(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.applyFill(newCanvasBackend(ctx), &CTColor{Value: color.RGBA{R: 10, G: 20, B: 30, A: 255}, HasValue: true}, nil)
	if ctx.Style.Fill.Color != (color.RGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("expected explicit fill color, got %v", ctx.Style.Fill.Color)
	}
}

func TestApplyStrokeDefaultsToBlack(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.applyStroke(newCanvasBackend(ctx), nil, &models.CtPath{})
	if ctx.Style.Stroke.Color != geom.Black {
		t.Fatalf("expected black default stroke, got %v", ctx.Style.Stroke.Color)
	}
}

func TestPathStyleObjectPropertiesOverrideDrawParam(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	object := &models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth: 2,
			Cap:       "Butt",
			Join:      "Bevel",
		},
	}
	dp := &models.DrawParam{
		LineWidth: 4,
		Cap:       "Round",
		Join:      "Round",
	}

	document.updateCtPathStyle(newCanvasBackend(ctx), object, dp, 1)

	if ctx.Style.StrokeWidth != 2 {
		t.Fatalf("stroke width = %g, want 2", ctx.Style.StrokeWidth)
	}
	if ctx.Style.StrokeCapper != canvas.ButtCap {
		t.Fatalf("stroke cap = %v, want butt", ctx.Style.StrokeCapper)
	}
	if ctx.Style.StrokeJoiner != canvas.BevelJoin {
		t.Fatalf("stroke join = %v, want bevel", ctx.Style.StrokeJoiner)
	}
}

func TestMiterLimitUsesAbsoluteMillimetres(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.updateCtPathStyle(newCanvasBackend(ctx), &models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth:  3,
			Join:       "Miter",
			MiterLimit: 2,
		},
	}, nil, 1)

	joiner, ok := ctx.Style.StrokeJoiner.(canvas.MiterJoiner)
	if !ok {
		t.Fatalf("stroke joiner = %T, want canvas.MiterJoiner", ctx.Style.StrokeJoiner)
	}
	if joiner.Limit != 2.0/1.5 {
		t.Fatalf("miter limit ratio = %g, want %g", joiner.Limit, 2.0/1.5)
	}
	geomJoiner := geom.MiterJoiner{GapJoiner: geom.BevelJoin, Limit: joiner.Limit}
	path := geom.MustParseSVGPath("M20 35L40 5L60 35")
	clipped := path.Stroke(3, geom.ButtCap, geomJoiner, geom.Tolerance).ToSVG()
	unclipped := path.Stroke(3, geom.ButtCap,
		geom.MiterJoiner{GapJoiner: geom.BevelJoin, Limit: 10.0 / 1.5}, geom.Tolerance).ToSVG()
	if clipped == unclipped {
		t.Fatal("different MiterLimit values produced identical stroked paths")
	}
}

func TestStrokeParametersRejectInvalidValues(t *testing.T) {
	ctx := canvas.NewContext(canvas.New(10, 10))
	var document Document
	document.updateCtPathStyle(newCanvasBackend(ctx), &models.CtPath{
		CTGraphicUnit: models.CTGraphicUnit{
			LineWidth:   -1,
			MiterLimit:  -1,
			DashOffset:  -1,
			DashPattern: &models.StArrayF{0, 0},
		},
	}, nil, 1)

	if ctx.Style.StrokeWidth != defaultLineWidth {
		t.Fatalf("stroke width = %g, want default %g", ctx.Style.StrokeWidth, defaultLineWidth)
	}
	if len(ctx.Style.Dashes) != 0 || ctx.Style.DashOffset != 0 {
		t.Fatalf("invalid dash pattern was retained: offset=%g dashes=%v", ctx.Style.DashOffset, ctx.Style.Dashes)
	}
}

func TestValidDashPattern(t *testing.T) {
	pattern := models.StArrayF{2, 1}
	if !validDashPattern(0, &pattern) {
		t.Fatal("valid dash pattern was rejected")
	}
	for _, invalid := range []models.StArrayF{{-1, 1}, {0, 0}} {
		if validDashPattern(0, &invalid) {
			t.Fatalf("invalid dash pattern was accepted: %v", invalid)
		}
	}
}
