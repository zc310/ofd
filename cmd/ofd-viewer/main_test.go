package main

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestCollectViewerPagesUsesGlobalSequence(t *testing.T) {
	documents := []*render.Document{
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}, {}}}),
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}}),
	}

	pages := collectViewerPages(documents)
	if len(pages) != 3 {
		t.Fatalf("page count = %d, want 3", len(pages))
	}
	if pages[0].document != documents[0] || pages[0].page != documents[0].Pages[0] {
		t.Fatal("global page 1 does not map to document 1 page 1")
	}
	if pages[2].document != documents[1] || pages[2].page != documents[1].Pages[0] {
		t.Fatal("global page 3 does not map to document 2 page 1")
	}
}

func TestDocumentTitlePrefersMetadataAndFallsBackToFileName(t *testing.T) {
	title := "文档标题"
	ofd := &parser.OFD{OFD: models.OFD{
		DocBodies: []models.DocBody{{DocInfo: models.DocInfo{Title: &title}}},
	}}
	if got := documentTitle(ofd, "document.ofd"); got != title {
		t.Fatalf("document title = %q, want %q", got, title)
	}

	if got := documentTitle(&parser.OFD{}, "document.ofd"); got != "document.ofd" {
		t.Fatalf("fallback title = %q, want %q", got, "document.ofd")
	}
	if got := documentTitle(nil, ""); got != "未加载文档" {
		t.Fatalf("empty title = %q, want %q", got, "未加载文档")
	}
}
