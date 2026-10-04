package validator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// validatorBenchFile 读取 testdata 下的 OFD 文档；缺失时跳过基准。
func validatorBenchFile(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		b.Skipf("测试文档 %s 不可用: %v", name, err)
	}
	return data
}

// BenchmarkValidate 测量 OFD 校验的完整成本，包括 ZIP 解压、XML 解析、
// 引用闭包、语义检查和摘要检查。Strict 与 Structural 两组结果用于区分
// XSD 校验在整体耗时和分配中的占比。
func BenchmarkValidate(b *testing.B) {
	for _, mode := range []struct {
		name string
		mode Mode
	}{
		{name: "Strict", mode: ModeStrict},
		{name: "Structural", mode: ModeStructural},
	} {
		b.Run(mode.name, func(b *testing.B) {
			for _, document := range []string{"helloworld.ofd", "ano.ofd", "1000-pages.ofd"} {
				b.Run(document, func(b *testing.B) {
					data := validatorBenchFile(b, document)
					validator, err := New(WithMode(mode.mode))
					if err != nil {
						b.Fatal(err)
					}
					b.SetBytes(int64(len(data)))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						report := validator.ValidateReader(context.Background(), bytes.NewReader(data), document)
						if report.Status == "" {
							b.Fatal("校验报告状态为空")
						}
					}
				})
			}
		})
	}
}
