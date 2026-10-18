package fastxml

import (
	"bytes"
	"strings"
	"testing"

	txml "github.com/tdewolff/parse/v2/xml"
)

var benchUnescapeOutput string

var benchLexerOutput *Lexer

var benchUnescapeInputs = []struct {
	name  string
	value string
}{
	{"ASCII14", "benchmark text"},
	{"CJK", "中文正文与标点，这是用于基准测试的文本内容。"},
	{"Plain1KiB", strings.Repeat("a", 1024)},
	{"Entities", "a&amp;b&lt;c&gt;d&#x1F600;"},
	{"SparseEntities", strings.Repeat("甲a", 256) + "&amp;"},
	{"Decimal", strings.Repeat("&#65;&#20013;", 16)},
	{"Hex", strings.Repeat("&#x41;&#x1F600;", 16)},
	{"InvalidNumeric", strings.Repeat("&#xZZ;&#4294967296;", 16)},
	{"Unknown", strings.Repeat("&unknown;", 16)},
}

func BenchmarkNewLexer(b *testing.B) {
	data := []byte(`<TextCode X="0" Y="3">正文</TextCode>`)
	b.ReportAllocs()
	for b.Loop() {
		benchLexerOutput = NewLexer(data)
	}
}

func BenchmarkLexer(b *testing.B) {
	data := []byte(`<Root>` + strings.Repeat(`<ofd:Item ID="7" Font="12" Boundary="1 2 30 40" Size="3" Name="甲&amp;乙"><Text>正文</Text></ofd:Item>`, 128) + `</Root>`)
	for _, mode := range []string{"Next", "ReadAttrs", "SkipAttrs", "SpanElement"} {
		b.Run(mode, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				l := NewLexer(data)
				if mode == "SpanElement" {
					_, _, start := l.Next()
					if _, err := l.SpanElement(start); err != nil {
						b.Fatal(err)
					}
					continue
				}
				for {
					tt, _, _ := l.Next()
					if tt == txml.ErrorToken {
						break
					}
					if tt == txml.StartTagToken {
						var err error
						if mode == "ReadAttrs" {
							_, err = l.ReadAttrs(func(_, _ []byte) error { return nil })
						} else if mode == "SkipAttrs" {
							_, err = l.SkipAttrs()
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
}

func BenchmarkUnescape(b *testing.B) {
	for _, input := range benchUnescapeInputs {
		b.Run(input.name, func(b *testing.B) {
			data := []byte(input.value)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				benchUnescapeOutput = Unescape(data)
			}
		})
	}
}

func BenchmarkAppendUnescape(b *testing.B) {
	for _, input := range benchUnescapeInputs {
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
