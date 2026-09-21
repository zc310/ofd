package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/parser"
)

// TestOFDSealDocumentCached 回归：OFD 印章文档在多次渲染同一页面时必须只
// 解析/缓存一次，避免每次渲染都重新解包印章并重新加载其字体。
func TestOFDSealDocumentCached(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "999.ofd"))
	if err != nil {
		t.Skipf("999.ofd 不可用: %v", err)
	}
	defer ofd.Close()
	if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
		t.Skip("文档无页面")
	}
	doc := NewDocumentWithDPI(canvas.White, ofd.Documents[0], canvas.DPI(96))
	page := doc.Pages[0]
	for i := 0; i < 2; i++ {
		if _, err := doc.RasterizePage(page, BackendCanvas, canvas.DPI(96)); err != nil {
			t.Fatalf("第 %d 次渲染失败: %v", i+1, err)
		}
	}
	doc.sealMu.Lock()
	n := len(doc.sealDocs)
	doc.sealMu.Unlock()
	if n == 0 {
		t.Skip("该样例无 OFD 印章，缓存用例不适用")
	}
	if n != 1 {
		t.Fatalf("印章缓存条目 = %d, want 1", n)
	}
}

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
