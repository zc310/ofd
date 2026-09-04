package render

import (
	"image"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/parser"
)

func TestAnoStampAnnotationRendersText(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	pageAnnotations := doc.Annotations[1]
	if pageAnnotations == nil || len(pageAnnotations.Annots) == 0 {
		t.Fatal("page 1 stamp annotation is missing")
	}
	annot := pageAnnotations.Annots[0]
	if annot.Appearance == nil || annot.Appearance.Boundary == nil {
		t.Fatal("stamp annotation appearance boundary is missing")
	}
	if len(annot.Appearance.Items) == 0 || len(annot.Appearance.Items[0].Text.TextCode) == 0 {
		t.Fatal("stamp annotation text is missing")
	}

	renderDoc := NewDocument(canvas.White, doc)
	box := *annot.Appearance.Boundary
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	ctx.SetFillColor(canvas.White)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	renderDoc.Annot(ctx, annot, box)

	if pixels := countNonWhite(rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)); pixels < 100 {
		t.Fatalf("stamp annotation rendered only %d non-white pixels", pixels)
	}
}

func TestAnoPageIncludesStampAnnotation(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	page := doc.Pages[0]
	page.EnsurePhysicalBox()
	if doc.Annotations[page.ID] == nil {
		t.Fatalf("page %d annotations are missing", page.ID)
	}
	box := page.Area.PhysicalBox
	withAnnotation := canvas.New(box.Width, box.Height)
	withoutAnnotation := canvas.New(box.Width, box.Height)
	renderDoc := NewDocument(canvas.White, doc)
	withoutContext := canvas.NewContext(withoutAnnotation)
	withContext := canvas.NewContext(withAnnotation)
	withoutContext.SetFillColor(canvas.White)
	withoutContext.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	withContext.SetFillColor(canvas.White)
	withContext.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	renderDoc.drawPageBackground(withoutContext, box)
	for _, template := range page.Template {
		renderDoc.Template(withoutContext, template, box)
	}
	if page.Content != nil {
		renderDoc.drawLayers(withoutContext, page.Content.Layer, box)
	}
	renderDoc.PageContent(withContext, page, true)

	withImage := rasterizer.Draw(withAnnotation, canvas.DPI(72), canvas.DefaultColorSpace)
	withoutImage := rasterizer.Draw(withoutAnnotation, canvas.DPI(72), canvas.DefaultColorSpace)
	different := 0
	minX, minY := withImage.Bounds().Max.X, withImage.Bounds().Max.Y
	maxX, maxY := withImage.Bounds().Min.X, withImage.Bounds().Min.Y
	for y := withImage.Bounds().Min.Y; y < withImage.Bounds().Max.Y; y++ {
		for x := withImage.Bounds().Min.X; x < withImage.Bounds().Max.X; x++ {
			if withImage.At(x, y) != withoutImage.At(x, y) {
				different++
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	t.Logf("stamp pixels=%d bounds=%d,%d-%d,%d", different, minX, minY, maxX, maxY)
	if different < 100 {
		t.Fatalf("stamp annotation changed only %d page pixels", different)
	}
	if maxX-minX < 100 || maxY-minY < 100 {
		t.Fatalf("stamp annotation pixels occupy only %d,%d-%d,%d", minX, minY, maxX, maxY)
	}
}

func countNonWhite(img image.Image) int {
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r != 0xffff || g != 0xffff || b != 0xffff || a != 0xffff {
				count++
			}
		}
	}
	return count
}
