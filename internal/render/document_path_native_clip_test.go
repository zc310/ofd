package render

import (
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
)

type nativePathClipContext struct {
	*clippedPathCaptureContext

	accept bool
	path   *geom.Path
	clip   *geom.Path
	matrix geom.Matrix
}

func (c *nativePathClipContext) DrawClippedPath(path, clip *geom.Path) bool {
	c.path, c.clip, c.matrix = path, clip, c.CurrentMatrix()
	return c.accept
}

func TestNativePathClipKeepsExactRegionCurveAndFallback(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "原生裁剪", false: "后端拒绝后求交"}[accepted], func(t *testing.T) {
			path := geom.MustParseSVGPath("M0 5 C5 20 15 -10 20 5")
			clip := geom.Rectangle(10, 10).Translate(5, 0)
			beforePath, beforeClip := path.ToSVG(), clip.ToSVG()
			ctx := &nativePathClipContext{
				clippedPathCaptureContext: &clippedPathCaptureContext{DrawContext: newCanvasBackend(canvas.NewContext(canvas.New(40, 40)))},
				accept:                    accepted,
			}
			ctx.SetStrokeWidth(2)
			ctx.SetStrokeColor(canvas.Black)
			matrix := geom.Identity.Translate(3, 4).Shear(.2, .1)
			ctx.Transform(matrix)
			object := spec.PathObject{CtPath: spec.CtPath{Stroke: spec.NewOptionalBool(true)}}
			(&Document{}).drawClippedPath(ctx, path, clip, object)
			if ctx.path != path || ctx.clip != clip || ctx.matrix != matrix {
				t.Fatal("原生裁剪改变了曲线、精确裁剪区或当前矩阵")
			}
			if accepted && len(ctx.paths) != 0 {
				t.Fatal("原生裁剪成功后不应再次绘制展开轮廓")
			}
			if !accepted && (len(ctx.paths) != 1 || !clip.Bounds().Contains(ctx.paths[0].Bounds())) {
				t.Fatal("后端拒绝原生裁剪时丢失了求交回退")
			}
			if path.ToSVG() != beforePath || clip.ToSVG() != beforeClip {
				t.Fatal("裁剪改写了只读几何")
			}
		})
	}
}
