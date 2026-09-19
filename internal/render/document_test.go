package render

import (
	"image/color"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// TestPageBackgroundCoversFractionalPixelEdges 验证页面尺寸换算成非整数像素
// 时，栅格画布向上取整留下的边缘像素仍被背景完全覆盖。否则最后一行/列是
// 半透明背景色，在深色阅读背景上会显示为黑线。
func TestPageBackgroundCoversFractionalPixelEdges(t *testing.T) {
	page := parser.NewPage(models.PageContent{
		Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 230.0111, Height: 305.1528}},
	})
	// 50 DPI 下 230.0111mm ≈ 452.78px、305.1528mm ≈ 600.7px，均非整数。
	dpi := canvas.DPI(50)
	doc := NewDocumentWithDPI(color.White, &parser.Document{}, dpi)
	canvasPage, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	img := Rasterize(canvasPage, dpi, canvas.DefaultColorSpace)
	bounds := img.Bounds()
	check := func(x, y int) {
		t.Helper()
		r, g, b, a := img.RGBAAt(x, y).RGBA()
		if a>>8 != 255 {
			t.Fatalf("pixel (%d,%d) alpha = %d, want opaque", x, y, a>>8)
		}
		if r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
			t.Fatalf("pixel (%d,%d) = (%d,%d,%d), want white", x, y, r>>8, g>>8, b>>8)
		}
	}
	for x := 0; x < bounds.Dx(); x++ {
		check(x, 0)
		check(x, bounds.Dy()-1)
	}
	for y := 0; y < bounds.Dy(); y++ {
		check(0, y)
		check(bounds.Dx()-1, y)
	}
}

func TestPageUsesA4ForInvalidPhysicalBox(t *testing.T) {
	page := parser.NewPage(models.PageContent{
		Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 0, Height: 0}},
	})
	doc := &Document{
		background: color.Transparent,
		Document:   &parser.Document{},
	}

	canvasPage, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	if canvasPage.W != 210 || canvasPage.H != 297 {
		t.Fatalf("canvas size = %gx%g, want 210x297", canvasPage.W, canvasPage.H)
	}
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	if box != (models.StBox{Width: 210, Height: 297}) {
		t.Fatalf("PhysicalBox = %+v, want A4", box)
	}
}

func TestDocumentPageIsSafeForConcurrentCalls(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := NewDocument(color.Transparent, ofd.Documents[0])
	page := doc.Pages[0]
	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				canvasPage, err := doc.Page(page)
				if err != nil {
					t.Errorf("render page: %v", err)
					return
				}
				if canvasPage == nil || canvasPage.W <= 0 || canvasPage.H <= 0 {
					t.Errorf("invalid canvas size: %#v", canvasPage)
					return
				}
			}
		}()
	}
	wg.Wait()
}
