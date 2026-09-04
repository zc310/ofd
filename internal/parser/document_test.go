package parser

import (
	"encoding/xml"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestDrawParamSampleParsesAndResolvesStyles(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "test", "testdata", "drawparam.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	if len(ofd.Documents) != 1 {
		t.Fatalf("document count = %d, want 1", len(ofd.Documents))
	}
	doc := ofd.Documents[0]
	for _, test := range []struct {
		id        uint64
		lineWidth float64
		cap       string
		join      string
		stroke    string
	}{
		{10, 0.353, "Butt", "Miter", "0 0 0"},
		{12, 3, "Round", "Round", "0 0 200"},
		{20, 4, "Butt", "Miter", "0 100 255"},
		{21, 3, "Round", "Round", "255 100 0"},
		{22, 1, "Butt", "Miter", "0 180 0"},
	} {
		dp := doc.GetDrawParam(models.StID(test.id))
		if dp == nil {
			t.Fatalf("DrawParam %d is nil", test.id)
		}
		if dp.LineWidth != test.lineWidth || dp.Cap != test.cap || dp.Join != test.join {
			t.Fatalf("DrawParam %d = width %g cap %q join %q", test.id, dp.LineWidth, dp.Cap, dp.Join)
		}
		if dp.StrokeColor == nil || dp.StrokeColor.Value == nil || dp.StrokeColor.Value.String() != test.stroke {
			t.Fatalf("DrawParam %d stroke color = %#v, want %q", test.id, dp.StrokeColor, test.stroke)
		}
	}

	path := doc.Pages[0].Content.Layer[0].PathObject[1]
	if path.LineWidth != 1 {
		t.Fatalf("PathObject LineWidth = %g, want 1", path.LineWidth)
	}
	if path.StrokeColor == nil || path.StrokeColor.Value == nil {
		t.Fatal("PathObject StrokeColor was not parsed")
	}
	var miterPath *models.PathObject
	for i := range doc.Pages[1].Content.Layer[0].PathObject {
		path := &doc.Pages[1].Content.Layer[0].PathObject[i]
		if path.ID == 51 {
			miterPath = path
			break
		}
	}
	if miterPath == nil || miterPath.LineWidth != 3 {
		if miterPath == nil {
			t.Fatal("page 2 PathObject ID=51 was not parsed")
		}
		t.Fatalf("page 2 PathObject ID=51 LineWidth = %g, want 3", miterPath.LineWidth)
	}
	var drawParamPath *models.PathObject
	for i := range doc.Pages[3].Content.Layer[0].PathObject {
		path := &doc.Pages[3].Content.Layer[0].PathObject[i]
		if path.ID == 31 {
			drawParamPath = path
			break
		}
	}
	if drawParamPath == nil || drawParamPath.DrawParam != 18 {
		if drawParamPath == nil {
			t.Fatal("page 4 PathObject ID=31 was not parsed")
		}
		t.Fatalf("PathObject ID=31 DrawParam = %d, want 18", drawParamPath.DrawParam)
	}
}

func TestGetDrawParamPreservesExplicitZeroOverrides(t *testing.T) {
	var params models.DrawParams
	if err := xml.Unmarshal([]byte(`<DrawParams>
  <DrawParam ID="1" LineWidth="2" DashOffset="4" MiterLimit="8"/>
  <DrawParam ID="2" Relative="1" LineWidth="0" DashOffset="0" MiterLimit="0"/>
</DrawParams>`), &params); err != nil {
		t.Fatal(err)
	}

	drawParams := make(map[models.StID]*models.DrawParam, len(params.DrawParam))
	for _, param := range params.DrawParam {
		drawParams[param.ID] = param
	}
	doc := &Document{DrawParams: drawParams}
	got := doc.GetDrawParam(2)
	if got == nil {
		t.Fatal("resolved DrawParam is nil")
	}
	if got.LineWidth != 0 || got.DashOffset != 0 || got.MiterLimit != 0 {
		t.Fatalf("explicit zero overrides = width %g offset %g miter %g", got.LineWidth, got.DashOffset, got.MiterLimit)
	}
}
