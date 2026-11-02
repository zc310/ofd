package canvas

import (
	"bytes"
	"image"
	"image/color"
	"os/exec"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestNativeClippedPathPreservesHolesOpacityAndFollowingDrawing(t *testing.T) {
	if _, ok := any(canvas.New(1, 1)).(interface {
		RenderClippedPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix, clip *canvas.Path)
	}); !ok {
		t.Skip("当前canvas依赖尚未支持原生路径裁剪")
	}
	clip := geom.Rectangle(20, 10).Translate(5, 5).
		Append(geom.Rectangle(4, 4).Translate(13, 8)).Settle(geom.EvenOdd)
	path := geom.MustParseSVGPath("M0 10 L30 10")
	beforePath, beforeClip := path.ToSVG(), clip.ToSVG()
	scene := canvas.New(40, 30)
	backend := &canvasBackend{ctx: canvas.NewContext(scene), vector: true}
	backend.ClearFill()
	backend.SetStrokeColor(color.RGBA{R: 128, A: 128})
	backend.SetStrokeWidth(2)
	if !backend.DrawClippedPath(path, clip) {
		t.Fatal("矢量表面拒绝原生路径裁剪")
	}
	// 不支持原生裁剪的光栅回放应与改动前的展开求交逐像素一致。
	fallback := canvas.New(40, 30)
	style := backend.ctx.Style
	style.Fill, style.Stroke = style.Stroke, canvas.Paint{}
	fallback.RenderPath(path.Stroke(2, geom.ButtCap, geom.MiterJoin, geom.Tolerance).And(clip), style, canvas.Identity)
	first := rasterize(scene, canvas.DPMM(10), canvas.DefaultColorSpace, nil)
	second := rasterize(fallback, canvas.DPMM(10), canvas.DefaultColorSpace, nil)
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("记录原生路径裁剪改变了光栅回放")
	}
	// 原生裁剪不得泄漏到后续绘制；后续对象应同时恢复不透明度。
	backend.ClearStroke()
	backend.SetFillColor(canvas.Blue)
	backend.DrawPath(0, 0, geom.Rectangle(4, 4).Translate(30, 20))
	if path.ToSVG() != beforePath || clip.ToSVG() != beforeClip {
		t.Fatal("记录裁剪时修改了调用方的路径")
	}
	data := renderPDFText(t, 40, 30, func(r pdfTextRenderer) { scene.RenderTo(r) })
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少pdftoppm，跳过PDF裁剪像素检查")
	}
	pages, err := testutil.RasterizePDFPages(t.Context(), tool, data, 254)
	if err != nil {
		t.Fatal(err)
	}
	checkNativeClippedPathPixels(t, pages[0])
}

func checkNativeClippedPathPixels(t *testing.T, img image.Image) {
	t.Helper()
	for _, tc := range []struct {
		x, y int
		want color.NRGBA
	}{
		{20, 200, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{150, 200, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{280, 200, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{100, 200, color.NRGBA{R: 255, G: 127, B: 127, A: 255}},
		{320, 80, color.NRGBA{B: 255, A: 255}},
	} {
		got, _ := color.NRGBAModel.Convert(img.At(tc.x, tc.y)).(color.NRGBA)
		if got != tc.want {
			t.Fatalf("PDF像素(%d,%d)=%v，期望%v", tc.x, tc.y, got, tc.want)
		}
	}
}
