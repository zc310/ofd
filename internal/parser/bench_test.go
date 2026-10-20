package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func parserBenchFile(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		b.Skipf("测试文档 %s 不可用: %v", name, err)
	}
	return data
}

func parserBenchOFD(b *testing.B, name string) *OFD {
	b.Helper()
	ofd, err := NewOFD(parserBenchFile(b, name))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ofd.Close() })
	return ofd
}

// BenchmarkOpen 测量读取 OFD 容器、文档 XML、资源索引及页面目录的成本；
// 页面内容本身仍按需加载。
func BenchmarkOpen(b *testing.B) {
	for _, name := range []string{"ofd-creator/helloworld.ofd", "ofdrw/ano.ofd", "ofd-creator/1000-pages.ofd"} {
		b.Run(name, func(b *testing.B) {
			data := parserBenchFile(b, name)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ofd, err := NewOFD(data)
				if err != nil {
					b.Fatal(err)
				}
				if err := ofd.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPageMetadata 逐页读取尺寸元数据，不加载页面正文或资源。
func BenchmarkPageMetadata(b *testing.B) {
	ofd := parserBenchOFD(b, "ofd-creator/1000-pages.ofd")
	if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
		b.Fatal("测试文档没有页面")
	}
	pages := ofd.Documents[0].Pages
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page := pages[i%len(pages)]
		if _, err := page.PhysicalBoxMetadata(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPageLoad 逐页加载页面 XML 并立即释放租约；以 1000 页循环确保
// 页面缓存持续淘汰，测量冷加载路径而非同一页的缓存命中。
func BenchmarkPageLoad(b *testing.B) {
	ofd, err := NewOFDWithOptions(parserBenchFile(b, "ofd-creator/1000-pages.ofd"), Options{PageCacheCapacity: 1})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ofd.Close() })
	if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
		b.Fatal("测试文档没有页面")
	}
	pages := ofd.Documents[0].Pages
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lease, err := pages[i%len(pages)].AcquireLease()
		if err != nil {
			b.Fatal(err)
		}
		lease.Release()
	}
}

// BenchmarkTemplateLoad 循环访问超过缓存容量的模板，测量解析和缓存记账的成本。
func BenchmarkTemplateLoad(b *testing.B) {
	content := `<Page><Content><Layer ID="1">` + strings.Repeat(
		`<TextObject ID="2" Font="3" Size="3.5" Boundary="0 0 100 10"><TextCode X="0" Y="5">模板文字</TextCode></TextObject>`, 1000,
	) + `</Layer></Content></Page>`
	doc := templateTestDocument(b, 33, content)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := doc.LoadTemplate(models.StID(i%33 + 1)); err != nil {
			b.Fatal(err)
		}
	}
}
