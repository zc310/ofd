package main

import (
	"image/color"
	"testing"

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
