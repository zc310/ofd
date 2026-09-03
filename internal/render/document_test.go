package render

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestPageUsesA4ForInvalidPhysicalBox(t *testing.T) {
	page := &parser.Page{PageContent: models.PageContent{
		Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 0, Height: 0}},
	}}
	doc := &Document{
		background: color.Transparent,
		Document:   &parser.Document{Seals: map[models.StID][]*parser.SealInfo{}},
	}

	canvasPage, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	if canvasPage.W != 210 || canvasPage.H != 297 {
		t.Fatalf("canvas size = %gx%g, want 210x297", canvasPage.W, canvasPage.H)
	}
	if page.Area.PhysicalBox != (models.StBox{Width: 210, Height: 297}) {
		t.Fatalf("PhysicalBox = %+v, want A4", page.Area.PhysicalBox)
	}
}
