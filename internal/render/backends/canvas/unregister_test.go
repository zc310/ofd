package canvas

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func testFontBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	return data
}

func TestUnregisterFallbackFont(t *testing.T) {
	data := testFontBytes(t)
	if err := registerFallbackFont(data, "可删除族", 0); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if _, ok := fallbackRegistry["可删除族"]; !ok {
		t.Fatal("注册后应在注册表里")
	}
	if err := UnregisterFallbackFont("可删除族"); err != nil {
		t.Fatalf("反注册失败: %v", err)
	}
	if _, ok := fallbackRegistry["可删除族"]; ok {
		t.Fatal("反注册后应从注册表移除")
	}
}

func TestUnregisterFallbackFontWithOpenDocument(t *testing.T) {
	data := testFontBytes(t)
	if err := registerFallbackFont(data, "延迟删除族", 0); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	doc, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "testdata", "ofdrw/intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(doc.Documents[0])
	if err := fonts.UseFallbackFont("延迟删除族"); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterFallbackFont("延迟删除族"); err != nil {
		t.Fatalf("反注册失败: %v", err)
	}
	if _, ok := fallbackRegistry["延迟删除族"]; !ok {
		t.Fatal("文档仍持有引用时不应删除")
	}
	fonts.Close()
	if _, ok := fallbackRegistry["延迟删除族"]; ok {
		t.Fatal("文档关闭后应释放")
	}
}

func TestUnregisterFallbackFontConcurrent(t *testing.T) {
	data := testFontBytes(t)
	if err := registerFallbackFont(data, "并发删除族", 0); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "testdata", "ofdrw/intro.ofd"))
			if err != nil {
				return
			}
			fonts := NewFonts(doc.Documents[0])
			fonts.UseFallbackFont("并发删除族")
			fonts.Close()
		}()
	}
	wg.Wait()
	UnregisterFallbackFont("并发删除族")
	if _, ok := fallbackRegistry["并发删除族"]; ok {
		t.Fatal("全部关闭后应释放")
	}
}
