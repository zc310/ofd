package converter

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"

	_ "github.com/zc310/ofd/internal/render/backends/gg"
)

// TestImageRasterBackendGGProducesPNG 验证 RasterBackend("gg") 时 PNG 输出
// 经 gg 栅格后端生成，且为可解码的非空 PNG。
func TestImageRasterBackendGGProducesPNG(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("缺少测试文档: %v", err)
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := render.NewDocumentWithDPI(nil, ofd.Documents[0], 72)

	var buf bytes.Buffer
	writer := Writer(func(int) (io.WriteCloser, error) { return nopWriteCloser{&buf}, nil })
	if err := ImageDocument(doc, PNG(), writer, RasterBackend("gg"), Page(1)); err != nil {
		t.Fatalf("gg 栅格后端 PNG 输出失败: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("PNG 输出为空")
	}
	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("PNG 解码失败: %v", err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Fatalf("PNG 尺寸无效: %v", img.Bounds())
	}
}

// TestImageRasterBackendGGJPEG 验证 RasterBackend("gg") 同样支持 JPEG 输出。
func TestImageRasterBackendGGJPEG(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	if _, err := os.Stat(input); err != nil {
		t.Skipf("缺少测试文档: %v", err)
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := render.NewDocumentWithDPI(nil, ofd.Documents[0], 72)

	var buf bytes.Buffer
	writer := Writer(func(int) (io.WriteCloser, error) { return nopWriteCloser{&buf}, nil })
	if err := ImageDocument(doc, JPG(), writer, RasterBackend("gg"), Page(1)); err != nil {
		t.Fatalf("gg 栅格后端 JPEG 输出失败: %v", err)
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("JPEG 解码失败: %v", err)
	}
}
