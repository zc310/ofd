package converter

import (
	"bytes"
	"encoding/base64"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestHTMLDocumentsEmbedsPNGPage(t *testing.T) {
	doc := render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}})
	var output bytes.Buffer
	if err := HTMLDocuments([]*render.Document{doc}, &output); err != nil {
		t.Fatal(err)
	}
	content := output.String()
	if !strings.Contains(content, "<!doctype html>") || !strings.Contains(content, "data:image/png;base64,") {
		t.Fatalf("HTML output does not contain an embedded PNG page: %s", content)
	}
	if !strings.Contains(content, "width:210.0000mm") || !strings.Contains(content, "height:297.0000mm") {
		t.Fatalf("HTML output does not preserve page size: %s", content)
	}
}

func TestHTMLDocumentsEmbedsSVGPage(t *testing.T) {
	doc := render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}})
	var output bytes.Buffer
	if err := HTMLDocuments([]*render.Document{doc}, &output, HTMLSVG()); err != nil {
		t.Fatal(err)
	}
	content := output.String()
	if !strings.Contains(content, "<svg") {
		t.Fatalf("HTML output does not contain an embedded SVG page: %s", content)
	}
	if strings.Contains(content, "<?xml") {
		t.Fatalf("HTML output contains an XML declaration inside the body: %s", content)
	}
}

func TestHTMLDocumentsEmbedsJPGPage(t *testing.T) {
	doc := render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}})
	var output bytes.Buffer
	if err := HTMLDocuments([]*render.Document{doc}, &output, HTMLJPG()); err != nil {
		t.Fatal(err)
	}
	content := output.String()
	marker := "data:image/jpeg;base64,"
	start := strings.Index(content, marker)
	if start < 0 {
		t.Fatalf("HTML output does not contain an embedded JPG page")
	}
	encoded := content[start+len(marker):]
	encoded = encoded[:strings.IndexByte(encoded, '"')]
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("embedded JPG cannot be decoded: %v", err)
	}
}

func TestHTMLDocumentsSupportsSinglePage(t *testing.T) {
	doc := render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}, {}}})
	var output bytes.Buffer
	if err := HTMLDocuments([]*render.Document{doc}, &output, Page(2)); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "class=\"page\"") != 1 {
		t.Fatalf("page count = %d, want 1", strings.Count(output.String(), "class=\"page\""))
	}
}

func TestHTMLDocumentTitleUsesOFDMetadata(t *testing.T) {
	title := `报告 <2026>`
	ofd := &parser.OFD{OFD: models.OFD{DocBodies: []models.DocBody{{DocInfo: models.DocInfo{Title: &title}}}}}
	if got := htmlDocumentTitle(ofd); got != title {
		t.Fatalf("htmlDocumentTitle() = %q, want %q", got, title)
	}
	if got := htmlHeader(title); !strings.Contains(got, "<title>报告 &lt;2026&gt;</title>") {
		t.Fatalf("HTML title was not escaped: %s", got)
	}
}

func TestHTMLDocumentTitleFallsBackWhenMetadataIsEmpty(t *testing.T) {
	empty := "  "
	ofd := &parser.OFD{OFD: models.OFD{DocBodies: []models.DocBody{{DocInfo: models.DocInfo{Title: &empty}}}}}
	if got := htmlDocumentTitle(ofd); got != "OFD 文档" {
		t.Fatalf("htmlDocumentTitle() = %q, want default title", got)
	}
}
