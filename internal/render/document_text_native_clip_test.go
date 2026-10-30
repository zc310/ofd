package render

import (
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

// 原生裁剪不应构造文字轮廓：这个字体面故意禁止访问轮廓。
type nativeClipFace struct {
	metricsFontFace

	t *testing.T
}

func (f nativeClipFace) ToPath(string) *geom.Path {
	f.t.Fatal("原生文字裁剪不应展开字形轮廓")
	return nil
}

func (nativeClipFace) ShapedRun(value string) TextRun { return value }

type nativeClipContext struct {
	textClipMatrixContext

	run  TextRun
	clip *geom.Path
}

func (c *nativeClipContext) DrawClippedTextLine(run TextRun, clip *geom.Path) bool {
	c.run, c.clip = run, clip
	return true
}

func TestNativeTextClipSkipsOutlinesAndPreservesExactTransformedClip(t *testing.T) {
	for _, delta := range []float64{0.000001, 1} {
		matrix := geom.Identity.Translate(8, 3).Scale(2, 4)
		clip := geom.Rectangle(4-delta, 5).Translate(1, 2)
		deviceClip := clip.Copy().Transform(matrix)
		ctx := &nativeClipContext{textClipMatrixContext: textClipMatrixContext{matrix: matrix}}
		(&Document{}).drawTextInline(ctx, nativeClipFace{t: t}, "HELLO", deviceClip)
		if ctx.run != "HELLO" || ctx.clip == nil || !rectNear(ctx.clip.Bounds(), clip.Bounds()) {
			t.Fatalf("原生裁剪丢失文字或放宽精确边界: text=%v clip=%v", ctx.run, ctx.clip)
		}
		if !rectNear(deviceClip.Bounds(), clip.Copy().Transform(matrix).Bounds()) {
			t.Fatal("原生裁剪修改了共享设备裁剪路径")
		}
	}
}
