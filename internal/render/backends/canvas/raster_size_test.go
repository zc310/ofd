package canvas

import (
	"image"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 非整数像素的页面和裁剪蒙版必须使用相同尺寸。
func TestRasterAndAlphaMaskCoverFractionalPage(t *testing.T) {
	res := geom.DPI(200)
	scene := tcanvas.New(210, 140)
	img := rasterize(scene, res, nil, nil)
	mask := newAlphaMaskSurface(scene.W, scene.H, res).RasterAlpha()
	want := image.Rect(0, 0, 1654, 1103)
	if img.Bounds() != want || mask.Bounds() != want {
		t.Fatalf("图片尺寸 %v，蒙版尺寸 %v，期望 %v", img.Bounds(), mask.Bounds(), want)
	}
}

// 已经按像素裁剪的文字掩码不能因毫米与像素的浮点往返多出一行。
func TestAlphaMaskRetainsExplicitPixelBounds(t *testing.T) {
	res := geom.DPMM(7.3)
	const width, height = 17, 23
	mask := newAlphaMaskSurfacePixels(width/res.DPMM(), height/res.DPMM(), res, width, height)
	if want := image.Rect(0, 0, width, height); mask.RasterAlpha().Bounds() != want {
		t.Fatalf("蒙版尺寸 %v，期望 %v", mask.RasterAlpha().Bounds(), want)
	}
}
