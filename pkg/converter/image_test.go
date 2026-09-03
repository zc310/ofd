package converter

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestImageDocumentWithoutWriterPreservesNoOutputBehavior(t *testing.T) {
	doc := render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}})
	if err := ImageDocument(doc); err != nil {
		t.Fatal(err)
	}
}
