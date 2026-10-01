package pdf2ofd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkConvertStandardPDFs 测量 docs/standards 中标准 PDF 的完整 PDF 到 OFD
// 转换耗时。输入读取不计入计时，输出写入丢弃写入器，重点观察解析和转换开销。
func BenchmarkConvertStandardPDFs(b *testing.B) {
	for _, name := range []string{
		"GBT_33190-2016.pdf",
		"GBT_9704-2012.pdf",
		"GBT_42133-2022.pdf",
	} {
		b.Run(name, func(b *testing.B) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", "standards", name))
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := Convert(context.Background(), data, io.Discard, ""); err != nil {
					b.Fatalf("转换 %s 失败: %v", name, err)
				}
			}
		})
	}
}
