package render

import (
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

func TestAnnotWatermarkRendersBelowPageContent(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "watermark-layer",
		Pages: []creator.Page{{
			Items: []creator.Item{
				creator.Path{X: 0, Y: 0, Width: 210, Height: 297, Data: "M 0 0 L 210 0 L 210 297 L 0 297 C", Fill: true, FillColor: &creator.Color{R: 0, G: 0, B: 0}},
			},
		}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{
			ID: 1, Type: "Watermark", Creator: "layer-test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Boundary: &creator.Box{X: 0, Y: 0, Width: 210, Height: 297},
			Items: []creator.Item{
				creator.Path{X: 0, Y: 0, Width: 210, Height: 297, Data: "M 0 0 L 210 0 L 210 297 L 0 297 C", Fill: true, FillColor: &creator.Color{R: 255, G: 0, B: 0}},
			},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	renderDoc := NewDocument(canvas.White, ofd.Documents[0])
	page := ofd.Documents[0].Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	renderDoc.PageContent(newCanvasBackend(ctx), page, false)

	img := rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)
	// 中心像素被黑色页面内容覆盖（Watermark 绘制在内容之下），而不是红色。
	r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	if r != 0 || g != 0 || b != 0 {
		t.Fatalf("watermark annotated above page content: center pixel %d,%d,%d, want black", r>>8, g>>8, b>>8)
	}
}

func TestAnnotStampRendersAbovePageContent(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "stamp-layer",
		Pages: []creator.Page{{
			Items: []creator.Item{
				creator.Path{X: 0, Y: 0, Width: 210, Height: 297, Data: "M 0 0 L 210 0 L 210 297 L 0 297 C", Fill: true, FillColor: &creator.Color{R: 0, G: 0, B: 0}},
			},
		}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{
			ID: 1, Type: "Stamp", Creator: "layer-test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Boundary: &creator.Box{X: 0, Y: 0, Width: 210, Height: 297},
			Items: []creator.Item{
				creator.Path{X: 0, Y: 0, Width: 210, Height: 297, Data: "M 0 0 L 210 0 L 210 297 L 0 297 C", Fill: true, FillColor: &creator.Color{R: 255, G: 0, B: 0}},
			},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	renderDoc := NewDocument(canvas.White, ofd.Documents[0])
	page := ofd.Documents[0].Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	renderDoc.PageContent(newCanvasBackend(ctx), page, false)

	img := rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)
	// 中心像素被红色 Stamp 注解覆盖（非 Watermark 仍绘制在最上层）。
	if !isRedAt(img, img.Bounds().Dx()/2, img.Bounds().Dy()/2) {
		t.Fatalf("stamp annotation did not cover page content at center")
	}
}

func isRedAt(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r>>8 > 200 && g>>8 < 100 && b>>8 < 100
}

// TestAnnoStampAnnotationRendersText 确保既有的 ano.ofd 整页水印（Type=Stamp）仍绘制为最上层，
// 与 Watermark 分层改动互不影响。
func TestAnnoStampAnnotationRemainsVisible(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	page := doc.Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	renderDoc := NewDocument(canvas.White, doc)
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	ctx.SetFillColor(canvas.White)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	renderDoc.PageContent(newCanvasBackend(ctx), page, true)

	pixels := countNonWhite(rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace))
	if pixels < 100 {
		t.Fatalf("stamp annotation no longer covers page content: only %d non-white pixels", pixels)
	}
}
