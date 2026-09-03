package converter

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestTextDocumentExtractsPageTextInOrder(t *testing.T) {
	page := &parser.Page{PageContent: models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItem("第一段", true),
				textItem("第二", true),
				{Kind: models.PageItemBlock, Block: models.PageBlock{CTPageBlock: models.CTPageBlock{
					Items: []models.PageItem{textItem("嵌套文字", true)},
				}}},
			},
		}}}},
	}}

	var output bytes.Buffer
	if err := TextDocument(&parser.Document{Pages: []*parser.Page{page}}, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "第一段\n第二\n嵌套文字\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestTextDocumentSeparatesPagesAndSupportsPageSelection(t *testing.T) {
	doc := &parser.Document{Pages: []*parser.Page{
		{PageContent: models.PageContent{Content: textContent("第一页")}},
		{PageContent: models.PageContent{Content: textContent("第二页")}},
	}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "第一页\n\f\n第二页\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}

	output.Reset()
	if err := TextDocument(doc, &output, Page(2)); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "第二页\n"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestTextDocumentSkipsInvisibleText(t *testing.T) {
	content := &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
		Items: []models.PageItem{
			textItem("可见", true),
			textItem("不可见", false),
		},
	}}}}

	var output bytes.Buffer
	if err := TextDocument(&parser.Document{Pages: []*parser.Page{{PageContent: models.PageContent{Content: content}}}}, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "可见\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func textItem(value string, visible bool) models.PageItem {
	object := models.TextObject{CtText: models.CtText{TextCode: []models.TextCode{{Value: value}}}}
	object.Visible.Set(visible)
	return models.PageItem{Kind: models.PageItemText, Text: object}
}

func textContent(value string) *models.Content {
	return &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
		Items: []models.PageItem{textItem(value, true)},
	}}}}
}
