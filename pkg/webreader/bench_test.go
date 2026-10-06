package webreader

import (
	"image"
	"os"
	"path/filepath"
	"testing"
)

// 本文件的基准覆盖浏览器侧真正会反复调用的路径：打开、取文字、搜索、渲染。
//
// 这个包此前一个基准都没有，于是每次改动都无法判断是否退化——上面几项里
// 有一半是在补基准之后才发现的。基准的作用是让下一个人不用重新测量。
//
// 常规用法是挑一项跑，并给足迭代次数：
//
//	go test ./pkg/webreader -bench . -benchtime 3x
//	go test ./pkg/webreader -bench 'Open|Text' -benchmem
//
// 不要用 -benchtime 1x 读数字：首轮包含字体栈与渲染子系统的初始化，实测
// Open/helloworld.ofd 在 1x 下是 50ms / 70MB 分配，5x 下降到 0.1ms / 79KB
// 分配——两个数量级的差是初始化成本，不是这一项慢。1x 只适合确认"能跑"。
//
// 文档规模刻意选了三个量级：小文档（helloworld，约一页）、中（ano，真实排版
// 且有嵌入字体）、大（1000-pages，页数主导）。同一项在三者上的比例往往就是
// 瓶颈在哪里的答案。

// benchFile 读取 testdata 下的文档；缺失时跳过而不是失败——那些 fixture 不是
// 每个构建环境都有，跳过与"这个包坏了"是两件事。
func benchFile(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		b.Skipf("测试文档 %s 不可用: %v", name, err)
	}
	return data
}

// benchReader 打开一个 Reader 并注册清理。
func benchReader(b *testing.B, name string) *Reader {
	b.Helper()
	r, err := Open(benchFile(b, name))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = r.Close() })
	return r
}

// BenchmarkOpen 打开文档的成本。这是整条链路的固定开销，WASM 侧每次换文档都要付。
func BenchmarkOpen(b *testing.B) {
	for _, doc := range []string{"helloworld.ofd", "ofdrw/ano.ofd", "1000-pages.ofd"} {
		b.Run(doc, func(b *testing.B) {
			data := benchFile(b, doc)
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for i := 0; i < b.N; i++ {
				r, err := Open(data)
				if err != nil {
					b.Fatal(err)
				}
				_ = r.Close()
			}
		})
	}
}

// BenchmarkText 取一页文字。浏览器的文字层、页内查找、复制都走这里。
//
// 首次调用建立布局并入缓存，后续命中缓存；两者都计入，因为真实使用里两者都有。
// 要分开看就把 b.ResetTimer() 挪到预热之后。
func BenchmarkText(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Text(i % 100); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTextCold 每页都是首次：建文字布局的成本。改排版代码时看这项。
func BenchmarkTextCold(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 每次换一个远超缓存容量的页号，逼迫 LRU 逐出而非命中。
		if _, err := r.Text(i % 200); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearch 重复全文搜索：首轮建立索引，后续复用缓存索引。
func BenchmarkSearch(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Search("文"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchNoMatch 重复执行无命中搜索，首轮建立索引、后续命中索引缓存。
func BenchmarkSearchNoMatch(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Search("不存在的查询内容"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchNoMatchCold 每次清空搜索索引缓存，测量完整无命中扫描的成本。
func BenchmarkSearchNoMatchCold(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.search = newSearchCache(len(r.pages))
		if _, err := r.Search("不存在的查询内容"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchCold 每次清空搜索索引缓存，测量全页搜索与命中布局的冷路径。
func BenchmarkSearchCold(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.search = newSearchCache(len(r.pages))
		if _, err := r.Search("文"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchCached 先预热搜索索引，再测连续搜索复用索引与命中 run 布局的成本。
func BenchmarkSearchCached(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	if _, err := r.Search("文"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Search("文"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearchTableReuse 对比每个候选文字对象都重建 KMP 表与一次构建后复用。
// 该基准隔离了查询表本身的成本，便于确认 Search 中的复用优化没有被整本文档
// 的解析、缓存或结果构造成本掩盖。
func BenchmarkSearchTableReuse(b *testing.B) {
	needle := []rune("查询目标")
	text := []rune("这是一段用于搜索的中文文字，包含查询目标。")
	texts := make([][]rune, 1024)
	for index := range texts {
		texts[index] = text
	}

	b.Run("Rebuild", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for iteration := 0; iteration < b.N; iteration++ {
			for _, candidate := range texts {
				benchmarkSearchSink += indexRunesWithTable(candidate, needle, buildRuneSearchTable(needle))
			}
		}
	})
	b.Run("Reuse", func(b *testing.B) {
		table := buildRuneSearchTable(needle)
		b.ReportAllocs()
		b.ResetTimer()
		for iteration := 0; iteration < b.N; iteration++ {
			for _, candidate := range texts {
				benchmarkSearchSink += indexRunesWithTable(candidate, needle, table)
			}
		}
	})
}

var benchmarkSearchSink int

// BenchmarkRenderPage 渲染单页为 PNG。WASM 侧翻页的主成本。
func BenchmarkRenderPage(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	opts := RenderOptions{DPI: 96, Format: RenderPNG, Background: image.White}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.RenderPage(i%20, opts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderPDF 渲染单页为 PDF。矢量输出，供打印与导出。
//
// PDF 不在 RenderFormat 里：它走 RenderPDF/RenderPDFTo，页面主体保留文字与
// 矢量内容，与位图路径的成本结构完全不同，因此单列一项。
func BenchmarkRenderPDF(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	opts := RenderOptions{DPI: 96, Background: image.White}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.RenderPDF([]int{i % 20}, opts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderPagesPDF 一次渲染多页，走与逐页不同的批量路径。
func BenchmarkRenderPagesPDF(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	opts := RenderOptions{DPI: 96, Background: image.White}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.RenderPDF([]int{0, 1, 2, 3, 4}, opts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPages 列举全部页面元信息。WASM 初始化时走一次。
func BenchmarkPages(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Pages(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFonts 列出嵌入字体，供浏览器注入 @font-face。
//
// 这一项会触发字体解析与子系统加载，是首次打开时容易被忽略的一段。
func BenchmarkFonts(b *testing.B) {
	r := benchReader(b, "ofdrw/ano.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Fonts(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOutline 读取大纲树。
func BenchmarkOutline(b *testing.B) {
	r := benchReader(b, "1000-pages.ofd")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Outline(); err != nil {
			b.Fatal(err)
		}
	}
}
