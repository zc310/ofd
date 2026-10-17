package render

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// TestSignoutStampCompositeAnnotationRendersInk 回归：手写签名以 CompositeObject
// 形式的 Stamp 注解出现，annot 必须绘制复合图元，而不是像以前那样整类跳过。
func TestSignoutStampCompositeAnnotationRendersInk(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw", "signout.ofd"))
	if err != nil {
		t.Skipf("样例不可用: %v", err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	pageAnnotations := doc.GetAnnotation(1)
	if pageAnnotations == nil {
		t.Fatal("page 1 annotations are missing")
	}
	var annot *models.Annot
	for _, candidate := range pageAnnotations.Annots {
		if candidate.Appearance == nil || candidate.Appearance.Boundary == nil {
			continue
		}
		for _, item := range candidate.Appearance.Items {
			if item.Kind == models.PageItemComposite {
				annot = candidate
				break
			}
		}
		if annot != nil {
			break
		}
	}
	if annot == nil {
		t.Fatal("未找到复合图元形式的印章注解")
	}

	renderDoc := NewDocument(canvas.White, doc)
	box := *annot.Appearance.Boundary
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	ctx.SetFillColor(canvas.White)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	renderDoc.Annot(newCanvasBackend(ctx), annot, box)

	if pixels := countNonWhite(rasterizer.Draw(c, canvas.DPI(200), canvas.DefaultColorSpace)); pixels < 100 {
		t.Fatalf("手写签章注解只绘制了 %d 个非白像素", pixels)
	}
}

func TestHighlightAnnotationDefaultsOpaqueAppearanceToTranslucent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		typeName    models.AnnotType
		alpha       *uint8
		wantRedMin  uint8
		wantRedMax  uint8
		wantBlueMin uint8
		wantBlueMax uint8
	}{
		{"highlight missing alpha", models.AnnotTypeHighlight, nil, 115, 140, 115, 140},
		{"highlight opaque alpha", models.AnnotTypeHighlight, uint8ptr(255), 115, 140, 115, 140},
		{"highlight explicit opacity", models.AnnotTypeHighlight, uint8ptr(128), 115, 140, 115, 140},
		{"path remains opaque", models.AnnotTypePath, uint8ptr(255), 0, 10, 245, 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pathData, err := models.ParsePathData("M 0 0 L 10 0 L 10 10 L 0 10 C")
			if err != nil {
				t.Fatal(err)
			}
			annotation := &models.Annot{
				Type: tc.typeName,
				Appearance: &models.Appearance{
					Boundary: &models.StBox{Width: 10, Height: 10},
					CTPageBlock: models.CTPageBlock{Items: []models.PageItem{{
						Kind: models.PageItemPath,
						Path: &models.PathObject{CtPath: models.CtPath{
							CTGraphicUnit:   models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}, Alpha: tc.alpha},
							Fill:            true,
							FillColor:       &models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}},
							AbbreviatedData: pathData,
						}},
					}}},
				},
			}

			canvasImage := canvas.New(10, 10)
			ctx := canvas.NewContext(canvasImage)
			ctx.SetFillColor(color.RGBA{R: 255, A: 255})
			ctx.DrawPath(0, 0, canvas.Rectangle(10, 10))
			(&Document{}).Annot(newCanvasBackend(ctx), annotation, models.StBox{Width: 10, Height: 10})

			pixel := color.NRGBAModel.Convert(rasterizer.Draw(canvasImage, canvas.DPI(72), canvas.DefaultColorSpace).At(5, 5)).(color.NRGBA)
			if pixel.R < tc.wantRedMin || pixel.R > tc.wantRedMax || pixel.B < tc.wantBlueMin || pixel.B > tc.wantBlueMax {
				t.Fatalf("center pixel=%v, want red %d..%d blue %d..%d", pixel, tc.wantRedMin, tc.wantRedMax, tc.wantBlueMin, tc.wantBlueMax)
			}
		})
	}
}

func TestAnoStampAnnotationRendersText(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	pageAnnotations := doc.GetAnnotation(1)
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
	renderDoc.Annot(newCanvasBackend(ctx), annot, box)

	if pixels := countNonWhite(rasterizer.Draw(c, canvas.DPI(72), canvas.DefaultColorSpace)); pixels < 100 {
		t.Fatalf("stamp annotation rendered only %d non-white pixels", pixels)
	}
}

func TestAnoPageIncludesStampAnnotation(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw/ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	page := doc.Pages[0]
	pageContent := page.Content()
	if pageContent == nil {
		t.Fatal("页面内容为空")
	}
	templates := page.Template()
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	if doc.GetAnnotation(page.ID) == nil {
		t.Fatalf("page %d annotations are missing", page.ID)
	}
	withAnnotation := canvas.New(box.Width, box.Height)
	withoutAnnotation := canvas.New(box.Width, box.Height)
	renderDoc := NewDocument(canvas.White, doc)
	withoutContext := canvas.NewContext(withoutAnnotation)
	withContext := canvas.NewContext(withAnnotation)
	withoutContext.SetFillColor(canvas.White)
	withoutContext.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	withContext.SetFillColor(canvas.White)
	withContext.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
	renderDoc.drawPageBackground(newCanvasBackend(withoutContext), box)
	for _, template := range templates {
		renderDoc.Template(newCanvasBackend(withoutContext), template, box)
	}
	if pageContent != nil {
		renderDoc.drawLayers(newCanvasBackend(withoutContext), pageContent.Layer, box)
	}
	renderDoc.PageContent(newCanvasBackend(withContext), page, true)

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
