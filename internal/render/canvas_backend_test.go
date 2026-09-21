package render

import (
	"image"
	"testing"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render/testscene"
)

// TestCanvasBackendParity 验证 canvasBackend 对 canvas.Context 的逐方法
// 转发与直接绘制输出逐像素一致：同一场景分别用 *canvas.Context（现有
// 调用方式）和 DrawContext 绘制，栅格化后必须字节相等。
func TestCanvasBackendParity(t *testing.T) {
	fontPath := testscene.PickTestFont()
	if fontPath == "" {
		t.Skip("未找到可用测试字体")
	}

	direct := drawPageDirect(t, fontPath)
	viaBackend := drawPageBackend(t, fontPath)

	if len(direct.Pix) != len(viaBackend.Pix) {
		t.Fatalf("尺寸不一致: %d vs %d", len(direct.Pix), len(viaBackend.Pix))
	}
	ink := 0
	for i := 0; i < len(direct.Pix); i += 4 {
		if direct.Pix[i] != 255 || direct.Pix[i+1] != 255 || direct.Pix[i+2] != 255 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("场景内容为空，对比无意义")
	}
	diff := 0
	for i := range direct.Pix {
		if direct.Pix[i] != viaBackend.Pix[i] {
			diff++
		}
	}
	if diff != 0 {
		t.Fatalf("canvasBackend 与直接绘制存在 %d 字节差异", diff)
	}
}

func drawPageDirect(t *testing.T, fontPath string) *image.RGBA {
	c := canvas.New(200, 140)
	ctx := canvas.NewContext(c)
	ctx.SetCoordSystem(canvas.CartesianIV)
	testscene.SceneDirect(t, ctx, fontPath)
	return Rasterize(c, canvas.DPI(96), canvas.DefaultColorSpace)
}

func drawPageBackend(t *testing.T, fontPath string) *image.RGBA {
	c := canvas.New(200, 140)
	ctx := canvas.NewContext(c)
	ctx.SetCoordSystem(canvas.CartesianIV)
	testscene.SceneBackend(t, NewCanvasBackend(ctx), fontPath)
	return Rasterize(c, canvas.DPI(96), canvas.DefaultColorSpace)
}
