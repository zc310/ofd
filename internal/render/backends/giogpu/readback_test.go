package giogpu

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestNormalizeReadbackPreservesOpaquePixelsAndSubimageBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(3, 5, 7, 8))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 21, G: 87, B: 203, A: 255}), image.Point{}, draw.Src)
	img.SetRGBA(4, 6, color.RGBA{R: 188, A: 128})
	img.SetRGBA(5, 6, color.RGBA{R: 200, G: 10, B: 20})
	before := bytes.Clone(img.Pix)
	sub, ok := img.SubImage(image.Rect(4, 6, 6, 8)).(*image.RGBA)
	if !ok {
		t.Fatal("RGBA 子图类型错误")
	}
	normalizeReadback(sub)
	if got, want := img.RGBAAt(4, 6), (color.RGBA{R: 128, A: 128}); got != want {
		t.Errorf("半透明红色转换错误: got=%v want=%v", got, want)
	}
	if got := img.RGBAAt(5, 6); got != (color.RGBA{}) {
		t.Errorf("透明像素应清除 RGB: %v", got)
	}
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			if y == 6 && (x == 4 || x == 5) {
				continue
			}
			i := img.PixOffset(x, y)
			if !bytes.Equal(img.Pix[i:i+4], before[i:i+4]) {
				t.Errorf("不透明或子图外像素 (%d,%d) 被改写", x, y)
			}
		}
	}
}

func TestNormalizeReadbackAlwaysProducesPremultipliedChannels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for alpha := range 256 {
		for value := range 256 {
			img.SetRGBA(value, alpha, color.RGBA{R: uint8(value), G: uint8(value), B: uint8(value), A: uint8(alpha)})
		}
	}
	normalizeReadback(img)
	for alpha := range 255 {
		for value := range 256 {
			c := img.RGBAAt(value, alpha)
			if c.R > c.A || c.G > c.A || c.B > c.A || c.A != uint8(alpha) {
				t.Fatalf("非法预乘颜色: alpha=%d value=%d got=%v", alpha, value, c)
			}
		}
	}
}

func TestRasterizeTransparentColorsSurvivePNGAndGoCompositing(t *testing.T) {
	requireGPU(t)
	colors := []color.NRGBA{
		{R: 255, A: 128},
		{G: 255, A: 128},
		{B: 255, A: 64},
		{R: 80, G: 160, B: 200, A: 192},
	}
	scene := tcanvas.New(40, 10)
	ctx := tcanvas.NewContext(scene)
	for i, c := range colors {
		ctx.SetFillColor(c)
		ctx.DrawPath(float64(i*10), 0, tcanvas.Rectangle(10, 10))
	}
	before := Stats()
	img, err := Rasterize(scene, geom.DPMM(2))
	if err != nil {
		t.Fatalf("GPU 渲染失败: %v", err)
	}
	if after := Stats(); after.GPUFrames != before.GPUFrames+1 || after.CPUFallbacks != before.CPUFallbacks {
		t.Fatalf("严格入口应只记录 GPU 成功帧: before=%+v after=%+v", before, after)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	composited := image.NewRGBA(img.Bounds())
	draw.Draw(composited, composited.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(composited, composited.Bounds(), img, image.Point{}, draw.Over)
	for i, c := range colors {
		x, y := i*20+10, 10
		want, _ := color.RGBAModel.Convert(c).(color.RGBA)
		if got := img.RGBAAt(x, y); !colorNear(got, want, 2) {
			t.Errorf("颜色 %v 回读错误: got=%v want=%v", c, got, want)
		}
		if got, _ := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA); !colorNear(color.RGBA(got), color.RGBA(c), 3) {
			t.Errorf("PNG 未保持直通颜色: got=%v want=%v", got, c)
		}
		white := 255 - want.A
		want.R, want.G, want.B, want.A = want.R+white, want.G+white, want.B+white, 255
		if got := composited.RGBAAt(x, y); !colorNear(got, want, 2) {
			t.Errorf("Go 白底合成错误: got=%v want=%v", got, want)
		}
	}
}
