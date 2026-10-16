package fastxml

import (
	"bytes"
	"strings"
	"testing"
)

var benchUnescapeOutput string

func BenchmarkAppendUnescape(b *testing.B) {
	for _, input := range []struct {
		name  string
		value string
	}{
		{"ASCII14", "benchmark text"},
		{"CJK", "中文正文与标点，这是用于基准测试的文本内容。"},
		{"Plain1KiB", strings.Repeat("a", 1024)},
		{"Entities", "a&amp;b&lt;c&gt;d&#x1F600;"},
		{"SparseEntities", strings.Repeat("甲a", 256) + "&amp;"},
	} {
		b.Run(input.name, func(b *testing.B) {
			data := []byte(input.value)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				// 包含转为最终字符串的成本，与 TextCode 的正文累积路径一致。
				benchUnescapeOutput = string(AppendUnescape(nil, data))
			}
		})
	}
}

func BenchmarkAppendUnescapeUnterminated(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"4KiB", 4096}, {"8KiB", 8192}, {"16KiB", 16384}} {
		b.Run(size.name, func(b *testing.B) {
			data := bytes.Repeat([]byte("&"), size.n)
			dst := make([]byte, 0, len(data))
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				dst = AppendUnescape(dst[:0], data)
			}
		})
	}
}
