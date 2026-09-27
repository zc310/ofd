package render

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestSealOpaqueWhiteBackgroundBecomesTransparent 回归：不带透明通道的印章
// 位图必须把白色纸张背景置为透明，红色墨迹保留；已经带透明通道的印章不能
// 被改写。
func TestSealOpaqueWhiteBackgroundBecomesTransparent(t *testing.T) {
	pal := color.Palette{color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{R: 223, G: 25, B: 29, A: 255}}
	src := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	for i, idx := range []uint8{0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0} {
		src.Pix[i] = idx
	}
	got := sealTransparentBackground(src)
	if _, _, _, a := got.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("白色背景像素 alpha = %d, want 0", a>>8)
	}
	if _, _, _, a := got.At(1, 0).RGBA(); a != 0xffff {
		t.Fatalf("红色墨迹像素 alpha = %d, want 255", a>>8)
	}
	if _, _, _, a := got.At(1, 1).RGBA(); a != 0xffff {
		t.Fatalf("红色墨迹像素 alpha = %d, want 255", a>>8)
	}

	transparent := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	transparent.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 128})
	if replaced := sealTransparentBackground(transparent); replaced != image.Image(transparent) {
		t.Fatal("带透明通道的印章不应被改写")
	}
}

// TestOpaqueSealFixtureBackgroundKeyedOut 用真实样例保护：签章内嵌的索引 PNG
// 没有透明通道、背景为纯白，按键出白色后左上角背景必须透明。
func TestOpaqueSealFixtureBackgroundKeyedOut(t *testing.T) {
	path := filepath.Join("..", "..", "test", "testdata", "ofd", "不规范资源路径.ofd")
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Skipf("样例不可用: %v", err)
	}
	defer zr.Close()

	var data []byte
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "SignValue.dat") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if data == nil {
		t.Skip("样例不含签章数据")
	}
	seal, err := parser.ExtractSealData(data)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(seal.Data))
	if err != nil {
		t.Fatal(err)
	}
	if opaque, ok := img.(interface{ Opaque() bool }); !ok || !opaque.Opaque() {
		t.Skip("签章本身带透明通道，本用例不适用")
	}
	b := img.Bounds()
	if _, _, _, a := sealTransparentBackground(img).At(b.Min.X, b.Min.Y).RGBA(); a != 0 {
		t.Fatalf("样例签章左上角背景 alpha = %d, want 0", a>>8)
	}
}

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
	doc := NewDocumentWithDPI(canvas.White, ofd.Documents[0], geom.DPI(96))
	page := doc.Pages[0]
	for i := range 2 {
		if _, err := doc.RasterizePage(page, BackendCanvas, geom.DPI(96)); err != nil {
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
	if err := RegisterFallbackFont(fontData, "Noto-Regular-Test", FontRegular); err != nil {
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
