package render

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/pkg/spec"
)

func compositeClipSource() *image.RGBA {
	parent := image.NewRGBA(image.Rect(-5, 10, 150, 130))
	for y := parent.Rect.Min.Y; y < parent.Rect.Max.Y; y++ {
		for x := parent.Rect.Min.X; x < parent.Rect.Max.X; x++ {
			a := [...]byte{0, 1, 63, 128, 255}[(x+5+y)%5]
			parent.SetRGBA(x, y, color.RGBA{R: a / 2, G: a / 3, B: a / 4, A: a})
		}
	}
	source, _ := parent.SubImage(image.Rect(10, 20, 138, 116)).(*image.RGBA)
	return source
}

func TestCompositeClipWindowKeepsMaskedPixelsBoundsAndSharedInputs(t *testing.T) {
	source := compositeClipSource()
	before := bytes.Clone(source.Pix)
	for _, clip := range []*geom.Path{
		geom.Rectangle(7.5, 63.25).Translate(119.25, 10.75),
		geom.Circle(18.5).Translate(60.25, 40.75),
		geom.Rectangle(30.25, 21.5).Translate(-12.75, 70.25),
		geom.Rectangle(1, 1).Translate(20, 20),
	} {
		originalPath := clip.Copy()
		region, local, ok := compositeClipRegion(source, clip)
		if !ok || region.Empty() || local == clip || !region.In(source.Rect) {
			t.Fatal("窄裁剪未使用合法像素窗口及裁剪路径副本")
		}
		var budget renderBudget
		budget.reset()
		got, ok := compositeImageWithClipAlpha(source, clip, 102, &budget)
		if !ok || got.Bounds() != source.Bounds() {
			t.Fatal("窗口合成改变了最终图片的画布尺寸或原点")
		}
		want := imageWithLocalClipAlpha(source, clip, 102)
		assertCompositeClipPixels(t, got, want)
		if !clip.Equals(originalPath) || !bytes.Equal(source.Pix, before) {
			t.Fatal("窗口合成修改了共享路径或源图像素")
		}
	}
}

func TestCompositeClipWindowSkipsSmallSavingsAndUnsupportedSources(t *testing.T) {
	source := compositeClipSource()
	for _, clip := range []*geom.Path{
		nil, {}, geom.Rectangle(128, 96), geom.Rectangle(120, 90).Translate(4, 3),
		geom.Rectangle(1, 1).Translate(1000, 1000), geom.Rectangle(1, 1).Translate(-1000, -1000),
	} {
		if _, _, ok := compositeClipRegion(source, clip); ok {
			t.Fatal("无需裁边或无相交范围时不应创建窗口")
		}
	}
	wrapped := struct{ image.Image }{source}
	if _, _, ok := compositeClipRegion(wrapped, geom.Rectangle(10, 20)); ok {
		t.Fatal("窗口推断不应解码或复制其他图片类型")
	}
}

type compositeClipCapture struct {
	DrawContext

	img    image.Image
	matrix geom.Matrix
}

func (*compositeClipCapture) CurrentMatrix() geom.Matrix { return geom.Identity }
func (c *compositeClipCapture) RenderImage(img image.Image, matrix geom.Matrix) {
	c.img, c.matrix = img, matrix
}

func TestCompositeClipPlacementKeepsCanvasAndChargesActualMaskPixels(t *testing.T) {
	doc := NewDocument(nil, nil, 0)
	defer doc.Close()
	source := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	for i := 3; i < len(source.Pix); i += 4 {
		source.Pix[i] = 255
	}
	box := spec.StBox{Width: 100, Height: 80}
	var object spec.CompositeObject
	object.Boundary, object.Alpha = box, new(uint8(102))
	var budget renderBudget
	budget.reset()
	capture := &compositeClipCapture{}
	if !doc.placeCompositeImage(capture, object, source, box, false, 100, 80, false, box, nil,
		geom.Rectangle(1, 80), &budget) || capture.img == nil {
		t.Fatal("窄裁剪的复合图元未绘制")
	}
	if capture.img.Bounds() != source.Bounds() {
		t.Fatal("最终位图尺寸改变，可能影响原生或 PDF 图像插值")
	}
	wantPixels := float64(1000*800 + 12*800)
	if used := budget.Usage().OffscreenPixels; math.Abs(used-wantPixels) > 1e-6 {
		t.Fatalf("预算没有按完整输出与窄掩码尺寸计量：%g，期望 %g", used, wantPixels)
	}
	c, _ := color.NRGBAModel.Convert(capture.img.At(5, 400)).(color.NRGBA)
	if c.A != 102 {
		t.Fatalf("复合对象整体透明度被改变：%d", c.A)
	}
}

func TestCompositeClipWindowChecksBudgetBeforeMaskAllocation(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 100, 80))
	var budget renderBudget
	budget.reset()
	if !budget.AllowOffscreenPixels(renderstate.MaxOffscreenPixels, 1, 25.4) {
		t.Fatal("预消费预算失败")
	}
	setAlphaMaskSurfaceFactory(t, func(float64, float64, geom.Resolution) AlphaMaskSurface {
		t.Fatal("预算拒绝后仍然创建覆盖率掩码")
		return nil
	})
	if img, ok := compositeImageWithClipAlpha(source, geom.Rectangle(1, 80), 102, &budget); ok || img != nil {
		t.Fatal("预算耗尽后仍然合成复合图元")
	}
}

func assertCompositeClipPixels(t *testing.T, a, b image.Image) {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatal("输出尺寸改变")
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				t.Fatalf("窗口合成改变了像素：(%d,%d) %v != %v", x, y, a.At(x, y), b.At(x, y))
			}
		}
	}
}

func TestCompositeClipWindowMatchesUntrimmedRasterAndPDF(t *testing.T) {
	source := compositeClipSource()
	src := cropImage(source, source.Rect.Min.X, source.Rect.Min.Y, source.Rect.Max.X, source.Rect.Max.Y)
	clip := geom.Circle(18.5).Translate(60.25, 40.75)
	scenes := make([]VectorSurface, 0, 2)
	for _, window := range []bool{false, true} {
		surface := clipTestSurface(t, func(ctx DrawContext) {
			for _, matrix := range []geom.Matrix{
				geom.Identity.Translate(1, 1).Scale(.2, .2),
				geom.Identity.Translate(20, 4).Rotate(20).Shear(.2, -.1).Scale(.2, .2),
				geom.Identity.Translate(37, 36).Scale(-.2, -.2),
			} {
				img := imageWithLocalClipAlpha(src, clip, 102)
				if window {
					var budget renderBudget
					budget.reset()
					var ok bool
					img, ok = compositeImageWithClipAlpha(src, clip, 102, &budget)
					if !ok {
						t.Fatal("窗口合成被拒绝")
					}
				}
				ctx.RenderImage(img, matrix)
			}
		})
		scenes = append(scenes, surface)
	}
	assertCompositeClipPixels(t, scenes[0].Rasterize(geom.DPI(144), nil), scenes[1].Rasterize(geom.DPI(144), nil))
	assertCompositeClipPixels(t, clipPDFImage(t, scenes[0], 144), clipPDFImage(t, scenes[1], 144))
}

func BenchmarkCompositeNarrowClipPlacement(b *testing.B) {
	doc := NewDocument(nil, nil, 0)
	defer doc.Close()
	source := image.NewRGBA(image.Rect(0, 0, 1245, 946))
	for i := 3; i < len(source.Pix); i += 4 {
		source.Pix[i] = 255
	}
	box := spec.StBox{Width: 329.35, Height: 250.12}
	var object spec.CompositeObject
	object.Boundary, object.Alpha = box, new(uint8(102))
	parentClip := geom.Rectangle(3, box.Height)
	capture := &compositeClipCapture{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var budget renderBudget
		budget.reset()
		if !doc.placeCompositeImage(capture, object, source, box, false, box.Width, box.Height,
			false, box, nil, parentClip, &budget) {
			b.Fatal("复合图元未绘制")
		}
	}
}
