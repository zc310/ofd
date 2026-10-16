package ofdxml

import (
	"bytes"
	"testing"
)

var benchWriterDoneOutput []byte

// BenchmarkWriterDone 覆盖缓冲区复用和超限移交两条完整文档输出路径。
func BenchmarkWriterDone(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"Small", 256}, {"256KiB", 256 << 10}, {"2MiB", 2 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			payload := bytes.Repeat([]byte("x"), size.n)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			for b.Loop() {
				w := New()
				w.Start("Root")
				w.Raw(payload)
				benchWriterDoneOutput = w.Done()
			}
		})
	}
}

// benchEscapeInputs 覆盖转义热路径的典型输入，供转义优化前后对比。
var benchEscapeInputs = []struct {
	name  string
	value string
}{
	{"ASCII", "Plain ASCII text with numbers 0123456789 and spaces."},
	{"CJK", "中文正文与标点，这是用于基准测试的文本内容。"},
	{"Mixed", "混合 mixed 文本 & special <chars> \"quoted\" 结束"},
	{"Entities", "a & b < c > d \" e ' f \t g \n h \r i"},
}

// BenchmarkAppendEscaped 分别测量文本与属性两种模式下的转义吞吐与分配，
// SetBytes 报告 MB/s 便于跨输入直接比较。
func BenchmarkAppendEscaped(b *testing.B) {
	for _, mode := range []struct {
		name string
		text bool
	}{{"Text", true}, {"Attr", false}} {
		for _, input := range benchEscapeInputs {
			b.Run(mode.name+"_"+input.name, func(b *testing.B) {
				w := &Writer{buf: make([]byte, 0, 4096)}
				b.SetBytes(int64(len(input.value)))
				b.ReportAllocs()
				for b.Loop() {
					w.buf = w.buf[:0]
					w.appendEscaped(input.value, mode.text)
				}
			})
		}
	}
}

// BenchmarkAppendFloat 测量 AppendFloat 整数快路径与小数兜底路径。
func BenchmarkAppendFloat(b *testing.B) {
	for _, value := range []struct {
		name  string
		value float64
	}{
		{"Integer", 210},
		{"Fraction", 4.2333333333},
		{"Small", -0.00001},
		{"Large", 1234567.0001},
	} {
		b.Run(value.name, func(b *testing.B) {
			dst := make([]byte, 0, 32)
			b.ReportAllocs()
			for b.Loop() {
				dst = AppendFloat(dst[:0], value.value)
			}
		})
	}
}
