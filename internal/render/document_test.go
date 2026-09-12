package render

import (
	"image/color"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

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
