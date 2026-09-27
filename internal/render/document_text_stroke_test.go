package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

func countColorNear(img image.Image, target color.RGBA, tol int) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			dr := int(cr>>8) - int(target.R)
			dg := int(cg>>8) - int(target.G)
			db := int(cb>>8) - int(target.B)
			if dr < 0 {
				dr = -dr
			}
			if dg < 0 {
				dg = -dg
			}
			if db < 0 {
				db = -db
			}
			if dr <= tol && dg <= tol && db <= tol {
				n++
			}
		}
	}
	return n
}

func renderCreatorPage(t *testing.T, txt creator.Text) image.Image {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID: "stroke",
		Pages: []creator.Page{{
			Area:  &creator.PageArea{PhysicalBox: &creator.Box{Width: 210, Height: 297}},
			Items: []creator.Item{txt},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	const dpi = 200.0
	doc := NewDocumentWithDPI(canvas.White, ofd.Documents[0], geom.DPI(dpi))
	r, err := doc.RasterizePage(doc.Pages[0], BackendCanvas, geom.DPI(dpi))
	if err != nil {
		t.Fatal(err)
	}
	return image.Image(r)
}

// 描边文字必须走路径绘制：Canvas 的原生文字接口在 canvas/text.go 的
// renderLineTo 中用 DefaultStyle 只复制 Fill，而 canvas.FontFace 没有描边字段，
// 因此 DrawText 会静默丢弃描边。text-directions.ofd 第 3 页 ID=51
// （Fill=true + Stroke=true）曾只渲染出浅黄填充，红色描边完全丢失。
func TestStrokedTextWithFillRendersStrokeColor(t *testing.T) {
	fill := float64Ptr(0)
	codeY := float64Ptr(20)
	fillOn := true
	img := renderCreatorPage(t, creator.Text{
		X: 20, Y: 173, Width: 170, Height: 20, Font: "楷体", Size: 14, Weight: 700,
		Stroke: true, Fill: &fillOn,
		FillColor:   &creator.Color{R: 255, G: 220, B: 100},
		StrokeColor: &creator.Color{R: 200, G: 50, B: 50},
		TextCodes:   []creator.TextCode{{X: fill, Y: codeY, Value: "描边+填充双重效果"}},
	})

	if n := countColorNear(img, color.RGBA{255, 220, 100, 255}, 8); n < 500 {
		t.Fatalf("浅黄填充像素 = %d，期望 >= 500（填充应可见）", n)
	}
	strokePx := countColorNear(img, color.RGBA{200, 50, 50, 255}, 8)
	if strokePx < 500 {
		t.Fatalf("红色描边像素 = %d，期望 >= 500；描边被静默丢弃", strokePx)
	}
}

// 空心字（Fill=false + Stroke=true）此前已走路径绘制，这里锁定其行为不被回归。
func TestHollowTextRendersOnlyStroke(t *testing.T) {
	fillOff := false
	codeX := float64Ptr(0)
	codeY := float64Ptr(20)
	img := renderCreatorPage(t, creator.Text{
		X: 20, Y: 148, Width: 170, Height: 20, Font: "楷体", Size: 14, Weight: 700,
		Stroke: true, Fill: &fillOff,
		StrokeColor: &creator.Color{R: 30, G: 100, B: 180},
		TextCodes:   []creator.TextCode{{X: codeX, Y: codeY, Value: "描边空心字"}},
	})

	if n := countColorNear(img, color.RGBA{30, 100, 180, 255}, 8); n < 500 {
		t.Fatalf("蓝色描边像素 = %d，期望 >= 500", n)
	}
	if n := countColorNear(img, color.RGBA{0, 0, 0, 255}, 8); n > 200 {
		t.Fatalf("黑色填充像素 = %d，空心字不应填充黑色", n)
	}
}

func float64Ptr(v float64) *float64 { return &v }

// 描边文字走路径绘制，因此 CTM 的倾斜/非等比缩放必须由 drawTextPath 自己
// 施加到字形轮廓上，不能只依赖原生文字分支的 ctx.Transform。
func TestStrokedTextCTMShearTiltsGlyphs(t *testing.T) {
	const shear = 1.0
	plain := textInkEdgesMM(t, renderCreatorPage(t, shearedStrokeText(0)))
	leaned := textInkEdgesMM(t, renderCreatorPage(t, shearedStrokeText(shear)))

	if plain.height <= 0 || leaned.height <= 0 {
		t.Fatalf("墨迹高度 单位CTM=%.1fmm 倾斜CTM=%.1fmm，期望都大于 0", plain.height, leaned.height)
	}
	delta := (leaned.topLeft - leaned.bottomLeft) - (plain.topLeft - plain.bottomLeft)
	// 与 TestTextCTMShearTiltsGlyphs 一致：正 shear 在对象空间里是上端偏左、
	// 下端偏右，上缘相对下缘的偏移量应减少约一个字高。
	if delta > -0.5*leaned.height {
		t.Fatalf("描边文字倾斜使上下左缘偏移增量 %+.1fmm，期望 <= -字高一半 %.1fmm（描边路径未按正确方向施加 CTM 线性部分）",
			delta, 0.5*leaned.height)
	}
}

func shearedStrokeText(shear float64) creator.Text {
	return creator.Text{
		X: 20, Y: 270, Width: 160, Height: 30, Font: "楷体", Size: 30,
		Fill: boolPtrT(), FillColor: &creator.Color{R: 0, G: 0, B: 0},
		Stroke:      true,
		StrokeColor: &creator.Color{R: 200, G: 50, B: 50},
		CTM:         &creator.CTM{1, 0, shear, 1, 0, 0},
		TextCodes:   []creator.TextCode{{X: float64Ptr(0), Y: float64Ptr(30), Value: "水"}},
	}
}
