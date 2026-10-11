// Package ofdxml 提供轻量的 OFD XML 顺序写入器。
package ofdxml

import (
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/zc310/ofd/pkg/spec"
)

// Namespace 是 OFD 命名空间 URI。
const Namespace = spec.Namespace

const prefix = "ofd"

var replacement = []byte("\uFFFD")

// Writer 将 OFD XML 顺序写入内存缓冲区，自动添加 ofd 前缀和根命名空间声明。
// 元素名必须是合法的 XML 本地名。属性必须在内容之前写入，内容之后的属性调用
// 会被忽略。一个 Writer 不能被多个 goroutine 同时使用。
type Writer struct {
	buf   []byte
	stack []string
	open  bool
}

// New 创建可复用的 OFD XML 写入器。
func New() *Writer {
	w := &Writer{
		buf:   make([]byte, 0, 4096),
		stack: make([]string, 0, 8),
	}
	w.Reset()
	return w
}

// Reset 清空文档并重新写入 XML 声明，复用已分配的缓冲区。
func (w *Writer) Reset() {
	w.buf = w.buf[:0]
	clear(w.stack)
	w.stack = w.stack[:0]
	w.open = false
	w.buf = append(w.buf, `<?xml version="1.0" encoding="UTF-8"?>`...)
}

// StartElement 开始一个 OFD 元素，根元素自动声明 OFD 命名空间。
func (w *Writer) StartElement(name string) {
	w.closeStart()
	w.buf = append(w.buf, '<')
	w.buf = append(w.buf, prefix...)
	w.buf = append(w.buf, ':')
	w.buf = append(w.buf, name...)
	if len(w.stack) == 0 {
		w.buf = append(w.buf, ` xmlns:ofd="`...)
		w.buf = append(w.buf, Namespace...)
		w.buf = append(w.buf, '"')
	}
	w.stack = append(w.stack, name)
	w.open = true
}

// Attribute 在当前尚未闭合的开始标签上写入属性。
func (w *Writer) Attribute(name, value string) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.appendEscaped(value, false)
	w.buf = append(w.buf, '"')
}

// Start 是 StartElement 的简写。
func (w *Writer) Start(name string) { w.StartElement(name) }

// Attr 是 Attribute 的简写，OFD 普通属性不添加前缀。
func (w *Writer) Attr(name, value string) { w.Attribute(name, value) }

// End 是 EndElement 的简写。
func (w *Writer) End() { w.EndElement() }

// AttrUint 写入十进制无符号整数属性，不生成临时字符串。
func (w *Writer) AttrUint(name string, value uint64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = strconv.AppendUint(w.buf, value, 10)
	w.buf = append(w.buf, '"')
}

// AttrInt 写入十进制有符号整数属性，不生成临时字符串。
func (w *Writer) AttrInt(name string, value int) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	w.buf = append(w.buf, '"')
}

// AttrFloat 写入最多保留四位小数的 OFD 数值属性。
func (w *Writer) AttrFloat(name string, value float64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = AppendFloat(w.buf, value)
	w.buf = append(w.buf, '"')
}

// AttrBox 写入由空格分隔的四个 OFD 矩形数值。
func (w *Writer) AttrBox(name string, x, y, width, height float64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = AppendFloat(w.buf, x)
	w.buf = append(w.buf, ' ')
	w.buf = AppendFloat(w.buf, y)
	w.buf = append(w.buf, ' ')
	w.buf = AppendFloat(w.buf, width)
	w.buf = append(w.buf, ' ')
	w.buf = AppendFloat(w.buf, height)
	w.buf = append(w.buf, '"')
}

// AttrFloatList 写入由空格分隔的 OFD 数值列表。
func (w *Writer) AttrFloatList(name string, values []float64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	for index, value := range values {
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = AppendFloat(w.buf, value)
	}
	w.buf = append(w.buf, '"')
}

// AttrIntList 写入由空格分隔的十进制整数列表。
func (w *Writer) AttrIntList(name string, values []int) {
	if !w.open {
		return
	}
	w.startAttr(name)
	for index, value := range values {
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	}
	w.buf = append(w.buf, '"')
}

func (w *Writer) startAttr(name string) {
	w.buf = append(w.buf, ' ')
	w.buf = append(w.buf, name...)
	w.buf = append(w.buf, '=', '"')
}

// Text 写入自动转义的文本，并将非法 XML 字符替换为 U+FFFD。
func (w *Writer) Text(value string) {
	w.closeStart()
	w.appendEscaped(value, true)
}

// Raw 原样追加已校验的 XML 片段，不进行转义。
// 调用方必须按当前命名空间上下文校验片段，保证标签配对，且不含 XML 声明和 DOCTYPE。
func (w *Writer) Raw(data []byte) {
	w.closeStart()
	w.buf = append(w.buf, data...)
}

// Element 写入只包含文本的 OFD 元素。
func (w *Writer) Element(name, value string) {
	w.StartElement(name)
	w.Text(value)
	w.EndElement()
}

// EndElement 关闭最近打开的元素，无内容的元素输出为自闭合标签。
func (w *Writer) EndElement() {
	if len(w.stack) == 0 {
		return
	}
	name := w.stack[len(w.stack)-1]
	w.stack[len(w.stack)-1] = ""
	w.stack = w.stack[:len(w.stack)-1]
	if w.open {
		w.buf = append(w.buf, '/', '>')
		w.open = false
		return
	}
	w.buf = append(w.buf, '<', '/')
	w.buf = append(w.buf, prefix...)
	w.buf = append(w.buf, ':')
	w.buf = append(w.buf, name...)
	w.buf = append(w.buf, '>')
}

// Bytes 关闭剩余元素并直接返回缓冲区，不复制字节。
// 返回值只在下一次写入或 Reset 前有效；需要长期保留时应自行复制。
func (w *Writer) Bytes() []byte {
	for len(w.stack) > 0 {
		w.EndElement()
	}
	return w.buf
}

// WriteTo 将完整 XML 文档写入 dst，并报告底层写入错误或短写入。
func (w *Writer) WriteTo(dst io.Writer) (int64, error) {
	data := w.Bytes()
	n, err := dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

func (w *Writer) closeStart() {
	if w.open {
		w.buf = append(w.buf, '>')
		w.open = false
	}
}

func (w *Writer) appendEscaped(value string, textMode bool) {
	for index := 0; index < len(value); {
		plain := index
		for index < len(value) && value[index] < utf8.RuneSelf && !escapeSeek[value[index]] {
			index++
		}
		w.buf = append(w.buf, value[plain:index]...)
		if index == len(value) {
			return
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			w.buf = append(w.buf, replacement...)
			index++
			continue
		}
		switch r {
		case '&':
			w.buf = append(w.buf, "&amp;"...)
		case '<':
			w.buf = append(w.buf, "&lt;"...)
		case '>':
			if textMode {
				w.buf = append(w.buf, "&gt;"...)
			} else {
				w.buf = append(w.buf, '>')
			}
		case '"':
			if textMode {
				w.buf = append(w.buf, '"')
			} else {
				w.buf = append(w.buf, "&quot;"...)
			}
		case '\r':
			// XML 会把字面回车和 CRLF 归一化为换行，字符引用才能保留原值。
			w.buf = append(w.buf, "&#xD;"...)
		case '\t', '\n':
			if textMode {
				w.buf = append(w.buf, byte(r))
			} else {
				w.buf = append(w.buf, "&#x"...)
				switch r {
				case '\t':
					w.buf = append(w.buf, '9')
				case '\n':
					w.buf = append(w.buf, 'A')
				}
				w.buf = append(w.buf, ';')
			}
		default:
			if isXMLChar(r) {
				w.buf = append(w.buf, value[index:index+size]...)
			} else {
				w.buf = append(w.buf, replacement...)
			}
		}
		index += size
	}
}

var escapeSeek = func() (table [256]bool) {
	for b := 0; b < 0x20; b++ {
		table[b] = true
	}
	for _, b := range []byte("&<>\"") {
		table[b] = true
	}
	return table
}()

func isXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}
