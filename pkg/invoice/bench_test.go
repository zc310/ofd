package invoice

import (
	"path/filepath"
	"testing"
)

// benchmarkInvoice 构造一次抽取消一次全流程开销使用的 OFD 字节。
func benchmarkInvoice(b *testing.B) []byte {
	b.Helper()
	return buildInvoiceOFD(b, "Doc_0/Attachs/original_invoice.xml", sampleAttachment, "")
}

// BenchmarkExtractBytes 衡量仅传入内存字节时的抽取开销。
func BenchmarkExtractBytes(b *testing.B) {
	data := benchmarkInvoice(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := Extract(data)
		if err != nil {
			b.Fatal(err)
		}
		if got.Number == "" {
			b.Fatal("抽取结果不应为空")
		}
	}
}

// BenchmarkExtractFilePath 衡量传入文件路径的抽取开销，包含文件读取。
func BenchmarkExtractFilePath(b *testing.B) {
	data := benchmarkInvoice(b)
	path := filepath.Join(b.TempDir(), "invoice.ofd")
	if err := writeFile(path, data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := Extract(path)
		if err != nil {
			b.Fatal(err)
		}
		if got.Number == "" {
			b.Fatal("抽取结果不应为空")
		}
	}
}
