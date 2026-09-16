package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/parser"
)

func Test999StampSealInheritsFallbackFont(t *testing.T) {
	sealDocument, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer sealDocument.Close()

	fontData, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}

	page := sealDocument.Documents[0].Pages[0]
	if _, err := page.PhysicalBox(); err != nil {
		t.Fatal(err)
	}
	withFallback := NewDocument(canvas.White, sealDocument.Documents[0])
	if err := RegisterFallbackFont(fontData, "Noto-Regular-Test", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := withFallback.UseFallbackFont("Noto-Regular-Test"); err != nil {
		t.Fatal(err)
	}
	if _, err := withFallback.Page(page); err != nil {
		t.Fatal(err)
	}
	if len(withFallback.fallbackFontFamilies()) != 1 {
		t.Fatalf("fallback families = %d, want 1", len(withFallback.fallbackFontFamilies()))
	}
	if withFallback.fallbackFontFamilies()[0] != "Noto-Regular-Test" {
		t.Fatalf("fallback family = %q", withFallback.fallbackFontFamilies()[0])
	}
}
