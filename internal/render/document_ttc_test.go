package render

import (
	"bytes"
	"github.com/zc310/ofd/internal/render/geom"
	"os"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

func TestCreatedTTCFontRendersEmbeddedCJKText(t *testing.T) {
	ttc, err := os.ReadFile("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc")
	if os.IsNotExist(err) {
		t.Skip("NotoSansCJK-Regular.ttc is not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	ofData, err := creator.Marshal(creator.Document{
		ID:       "ttc-render-test",
		Fonts:    []creator.Font{{Name: "Noto Sans CJK SC", Format: "ttc", Data: ttc}},
		PageSize: creator.PageSize{Width: 80, Height: 40},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{X: 5, Y: 25, Width: 60, Height: 15, Size: 12, Font: "Noto Sans CJK SC", Value: "你好"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(bytes.NewReader(ofData))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := NewDocument(canvas.White, ofd.Documents[0])
	page, err := document.Page(ofd.Documents[0].Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	rendered := page.Rasterize(geom.Resolution(canvas.DPI(36)))
	if rendered == nil || rendered.Bounds().Empty() {
		t.Fatal("rendered page is empty")
	}
	if countNonWhite(rendered) == 0 {
		t.Fatal("embedded CJK text produced no visible pixels")
	}
}
