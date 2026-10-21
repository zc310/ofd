// Package ofdxml 提供 OFD XML 顺序写入及读取、修改、写回支持。
package ofdxml

import (
	"errors"
	"io"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/zc310/ofd/pkg/spec"
)

// Namespace 是 OFD 命名空间 URI。
const Namespace = spec.Namespace

const prefix = "ofd"

var replacement = []byte("\uFFFD")

var errXMLCharacter = errors.New("XML 包含非法 UTF-8 或 XML 字符")

// Writer 将 OFD XML 顺序写入内存或分块写入目标，自动添加 ofd 前缀和根命名空间声明。
// OFD 元素名必须是合法的 XML 本地名，扩展元素可通过 StartQualified 写入。
// 属性必须在内容之前写入，内容之后的属性调用会被忽略。
// 一个 Writer 不能被多个 goroutine 同时使用。
type Writer struct {
	buf       []byte
	stack     []elementName
	open      bool
	dst       io.Writer
	err       error
	strictXML bool
}

type elementName struct {
	name  string
	space string
}

// New 从复用池获取 OFD XML 写入器。用完后应调用 Put 归还缓冲区；
// 若需要长期持有输出，使用 Done。
func New() *Writer {
	w := writerPool.Get().(*Writer)
	w.Reset()
	return w
}

// newWriter 分配一个新的写入器，作为池的构造函数。
func newWriter() *Writer {
	w := &Writer{
		buf:   make([]byte, 0, 4096),
		stack: make([]elementName, 0, 8),
	}
	w.Reset()
	return w
}

// maxPooledCapacity 限制归还池中保留的缓冲区容量，避免个别大文档长期占用内存。
const maxPooledCapacity = 1 << 20

const streamBufferSize = 32 << 10

var writerPool = sync.Pool{New: func() any { return newWriter() }}

// Put 将不再使用的 Writer 归还池中以复用缓冲区。归还后不得再访问 Bytes
// 返回的切片；Done 的返回值独立于写入器，可安全使用。
func Put(w *Writer) {
	if w == nil {
		return
	}
	if cap(w.buf) > maxPooledCapacity {
		w.buf = nil
	}
	w.dst = nil
	w.err = nil
	w.strictXML = false
	writerPool.Put(w)
}

// Done 关闭剩余元素并把 Writer 归还池中，调用后不得再使用该 Writer。
// 小缓冲区复制输出后复用，超过池容量上限的缓冲区直接移交给调用方。
// 返回的切片可安全长期持有；仅需立即写入时，改用 Bytes 避免复制。
func (w *Writer) Done() []byte {
	data := w.Bytes()
	if cap(data) > maxPooledCapacity {
		w.buf = nil
	} else {
		data = append([]byte(nil), data...)
	}
	Put(w)
	return data
}

// Reset 清空文档并重新写入 XML 声明，复用已分配的缓冲区。
func (w *Writer) Reset() {
	w.dst = nil
	w.err = nil
	w.strictXML = false
	w.buf = w.buf[:0]
	clear(w.stack)
	w.stack = w.stack[:0]
	w.open = false
	w.buf = append(w.buf, `<?xml version="1.0" encoding="UTF-8"?>`...)
}

// ResetTo 开始分块输出文档；写入过程中自动冲刷缓冲区，以 Finish 收尾并获取错误。
// dst 为 nil 时等同于 Reset。分块模式不使用 Bytes、Done 或 WriteTo 获取完整文档。
func (w *Writer) ResetTo(dst io.Writer) {
	if dst != nil && cap(w.buf) > 2*streamBufferSize {
		w.buf = nil
	}
	w.Reset()
	w.dst = dst
}

// Err 返回分块输出遇到的第一个错误。错误后不再调用底层写入器。
func (w *Writer) Err() error { return w.err }

// Finish 关闭剩余元素并写出最后一块，返回第一次写入错误或 io.ErrShortWrite。
func (w *Writer) Finish() error {
	w.closeElements()
	if w.dst != nil {
		w.flush()
		w.dst = nil
	}
	return w.err
}

func (w *Writer) flush() {
	if w.err == nil && len(w.buf) > 0 {
		n, err := w.dst.Write(w.buf)
		if err == nil && n != len(w.buf) {
			err = io.ErrShortWrite
		}
		w.err = err
	}
	w.buf = w.buf[:0]
}

func (w *Writer) maybeFlush() {
	if w.dst != nil && len(w.buf) >= streamBufferSize {
		w.flush()
	}
}

func (w *Writer) appendString(value string) {
	if w.dst == nil {
		w.buf = append(w.buf, value...)
		return
	}
	for len(value) > 0 && w.err == nil {
		w.maybeFlush()
		n := min(len(value), streamBufferSize-len(w.buf))
		w.buf = append(w.buf, value[:n]...)
		value = value[n:]
	}
}

// StartElement 开始一个 OFD 元素，根元素自动声明 OFD 命名空间。
func (w *Writer) StartElement(name string) {
	w.startElement(name, true)
}

// StartQualified 开始一个使用指定 XML 名称的扩展元素，不自动添加前缀。
// name 必须是合法的 XML 名称，例如 ext:Value 或 Value；调用方负责用 Attr
// 声明所需的命名空间。嵌套的 OFD 元素仍可通过 Start 写入。
func (w *Writer) StartQualified(name string) {
	w.startElement(name, false)
}

func (w *Writer) startElement(name string, ofd bool) {
	space := ""
	if ofd {
		space = prefix
	}
	w.startElementName(space, name, ofd)
}

// startElementName 直接写入分开的前缀与本地名，避免树写回时拼接临时字符串。
func (w *Writer) startElementName(space, name string, declareOFD bool) {
	if w.err != nil {
		return
	}
	w.closeStart()
	w.buf = append(w.buf, '<')
	if space != "" {
		w.appendString(space)
		w.buf = append(w.buf, ':')
	}
	w.appendString(name)
	if len(w.stack) == 0 && declareOFD {
		w.buf = append(w.buf, ` xmlns:ofd="`...)
		w.buf = append(w.buf, Namespace...)
		w.buf = append(w.buf, '"')
	}
	w.stack = append(w.stack, elementName{name: name, space: space})
	w.open = true
}

// Attribute 在当前尚未闭合的开始标签上写入属性。
func (w *Writer) Attribute(name, value string) {
	if !w.open || w.err != nil {
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
	if !w.open || w.err != nil {
		return
	}
	w.startAttr(name)
	w.buf = strconv.AppendUint(w.buf, value, 10)
	w.buf = append(w.buf, '"')
}

// AttrInt 写入十进制有符号整数属性，不生成临时字符串。
func (w *Writer) AttrInt(name string, value int) {
	if !w.open || w.err != nil {
		return
	}
	w.startAttr(name)
	w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	w.buf = append(w.buf, '"')
}

// AttrFloat 写入最多保留四位小数的 OFD 数值属性。
func (w *Writer) AttrFloat(name string, value float64) {
	if !w.open || w.err != nil {
		return
	}
	w.startAttr(name)
	w.buf = AppendFloat(w.buf, value)
	w.buf = append(w.buf, '"')
}

// AttrBox 写入由空格分隔的四个 OFD 矩形数值。
func (w *Writer) AttrBox(name string, x, y, width, height float64) {
	if !w.open || w.err != nil {
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
	if !w.open || w.err != nil {
		return
	}
	w.startAttr(name)
	for index, value := range values {
		if w.err != nil {
			return
		}
		w.maybeFlush()
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = AppendFloat(w.buf, value)
	}
	w.buf = append(w.buf, '"')
}

// AttrIntList 写入由空格分隔的十进制整数列表。
func (w *Writer) AttrIntList(name string, values []int) {
	if !w.open || w.err != nil {
		return
	}
	w.startAttr(name)
	for index, value := range values {
		if w.err != nil {
			return
		}
		w.maybeFlush()
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	}
	w.buf = append(w.buf, '"')
}

func (w *Writer) startAttr(name string) {
	w.startAttrName("", name)
}

func (w *Writer) startAttrName(space, name string) {
	w.maybeFlush()
	w.buf = append(w.buf, ' ')
	if space != "" {
		w.appendString(space)
		w.buf = append(w.buf, ':')
	}
	w.appendString(name)
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
	if w.dst == nil {
		w.buf = append(w.buf, data...)
		return
	}
	for len(data) > 0 && w.err == nil {
		w.maybeFlush()
		n := min(len(data), streamBufferSize-len(w.buf))
		w.buf = append(w.buf, data[:n]...)
		data = data[n:]
	}
}

// Directive 写入已校验的 XML 指令内容，例如 DOCTYPE 定义。
// value 不含外围的 <! 和 >；调用方负责保证内容符合 XML 语法。
func (w *Writer) Directive(value string) {
	w.closeStart()
	w.appendString("<!")
	w.appendString(value)
	w.appendString(">")
}

// Element 写入只包含文本的 OFD 元素。
func (w *Writer) Element(name, value string) {
	w.StartElement(name)
	w.Text(value)
	w.EndElement()
}

// TextUint 写入十进制无符号整数文本，不生成临时字符串。
func (w *Writer) TextUint(value uint64) {
	if w.err != nil {
		return
	}
	w.closeStart()
	w.buf = strconv.AppendUint(w.buf, value, 10)
}

// TextInt 写入十进制有符号整数文本，不生成临时字符串。
func (w *Writer) TextInt(value int) {
	if w.err != nil {
		return
	}
	w.closeStart()
	w.buf = strconv.AppendInt(w.buf, int64(value), 10)
}

// TextFloat 写入最多保留四位小数的 OFD 数值文本。
func (w *Writer) TextFloat(value float64) {
	if w.err != nil {
		return
	}
	w.closeStart()
	w.buf = AppendFloat(w.buf, value)
}

// TextBox 写入由空格分隔的四个 OFD 矩形数值文本。
func (w *Writer) TextBox(x, y, width, height float64) {
	if w.err != nil {
		return
	}
	w.closeStart()
	w.buf = AppendFloat(w.buf, x)
	w.buf = append(w.buf, ' ')
	w.buf = AppendFloat(w.buf, y)
	w.buf = append(w.buf, ' ')
	w.buf = AppendFloat(w.buf, width)
	w.buf = append(w.buf, ' ')
	w.buf = AppendFloat(w.buf, height)
}

// TextFloatList 写入由空格分隔的 OFD 数值列表文本。
func (w *Writer) TextFloatList(values []float64) {
	w.closeStart()
	for index, value := range values {
		if w.err != nil {
			return
		}
		w.maybeFlush()
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = AppendFloat(w.buf, value)
	}
}

// TextIntList 写入由空格分隔的十进制整数列表文本。
func (w *Writer) TextIntList(values []int) {
	w.closeStart()
	for index, value := range values {
		if w.err != nil {
			return
		}
		w.maybeFlush()
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	}
}

// ElementUint 写入只包含无符号整数的 OFD 元素。
func (w *Writer) ElementUint(name string, value uint64) {
	w.StartElement(name)
	w.TextUint(value)
	w.EndElement()
}

// ElementInt 写入只包含有符号整数的 OFD 元素。
func (w *Writer) ElementInt(name string, value int) {
	w.StartElement(name)
	w.TextInt(value)
	w.EndElement()
}

// ElementFloat 写入只包含 OFD 数值的元素。
func (w *Writer) ElementFloat(name string, value float64) {
	w.StartElement(name)
	w.TextFloat(value)
	w.EndElement()
}

// ElementBox 写入只包含矩形数值的元素。
func (w *Writer) ElementBox(name string, x, y, width, height float64) {
	w.StartElement(name)
	w.TextBox(x, y, width, height)
	w.EndElement()
}

// ElementFloatList 写入只包含 OFD 数值列表的元素。
func (w *Writer) ElementFloatList(name string, values []float64) {
	w.StartElement(name)
	w.TextFloatList(values)
	w.EndElement()
}

// ElementIntList 写入只包含整数列表的元素。
func (w *Writer) ElementIntList(name string, values []int) {
	w.StartElement(name)
	w.TextIntList(values)
	w.EndElement()
}

// EndElement 关闭最近打开的元素，无内容的元素输出为自闭合标签。
func (w *Writer) EndElement() {
	if len(w.stack) == 0 || w.err != nil {
		return
	}
	w.maybeFlush()
	name := w.stack[len(w.stack)-1]
	w.stack[len(w.stack)-1] = elementName{}
	w.stack = w.stack[:len(w.stack)-1]
	if w.open {
		w.buf = append(w.buf, '/', '>')
		w.open = false
		return
	}
	w.buf = append(w.buf, '<', '/')
	if name.space != "" {
		w.appendString(name.space)
		w.buf = append(w.buf, ':')
	}
	w.appendString(name.name)
	w.buf = append(w.buf, '>')
}

// Bytes 关闭剩余元素并直接返回缓冲区，不复制字节。
// 返回值只在下一次写入或 Reset 前有效；需要长期保留时应自行复制。
// 仅适用于内存模式，分块模式使用 Finish。
func (w *Writer) Bytes() []byte {
	w.closeElements()
	return w.buf
}

func (w *Writer) closeElements() {
	for len(w.stack) > 0 && w.err == nil {
		w.EndElement()
	}
}

// WriteTo 将已缓存在内存中的完整 XML 写入 dst，并报告底层写入错误或短写入。
// 需要在生成过程中控制缓冲区大小时，使用 ResetTo 和 Finish。
func (w *Writer) WriteTo(dst io.Writer) (int64, error) {
	data := w.Bytes()
	n, err := dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}

func (w *Writer) closeStart() {
	w.maybeFlush()
	if w.open {
		w.buf = append(w.buf, '>')
		w.open = false
	}
}

func (w *Writer) appendEscaped(value string, textMode bool) {
	for index := 0; index < len(value); {
		if w.err != nil {
			return
		}
		w.maybeFlush()
		plain := index
		for index < len(value) && value[index] < utf8.RuneSelf && !escapeSeek[value[index]] {
			index++
		}
		w.appendString(value[plain:index])
		if index == len(value) {
			return
		}
		if value[index] >= utf8.RuneSelf {
			// 连续多字节字符整体校验后一次追加，避免逐 rune 解码，
			// 中文正文等非 ASCII 文本吞吐可提升一倍以上。
			end := index
			for end < len(value) && value[end] >= utf8.RuneSelf {
				end++
			}
			run := value[index:end]
			if utf8.ValidString(run) && !containsNonXMLRune(run) {
				w.appendString(run)
				index = end
				continue
			}
			// 含非法 UTF-8 或非 XML 字符时回退逐 rune，只扫描本段避免退化。
			for index < end {
				if w.err != nil {
					return
				}
				w.maybeFlush()
				r, size := utf8.DecodeRuneInString(value[index:])
				if r == utf8.RuneError && size == 1 {
					w.invalidXMLCharacter()
					index++
					continue
				}
				if isXMLChar(r) {
					w.appendString(value[index : index+size])
				} else {
					w.invalidXMLCharacter()
				}
				index += size
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			w.invalidXMLCharacter()
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
				w.invalidXMLCharacter()
			}
		}
		index += size
	}
}

func (w *Writer) invalidXMLCharacter() {
	if w.strictXML {
		w.err = errXMLCharacter
	} else {
		w.buf = append(w.buf, replacement...)
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

// containsNonXMLRune 报告合法 UTF-8 片段是否含 XML 不允许的 U+FFFE/U+FFFF。
// 这两个码点是合法编码中仅有的非 ASCII 非法 XML 字符，其余非 ASCII 均已合法。
func containsNonXMLRune(run string) bool {
	for index := 0; index+2 < len(run); index++ {
		if run[index] == 0xEF && run[index+1] == 0xBF && (run[index+2] == 0xBE || run[index+2] == 0xBF) {
			return true
		}
	}
	return false
}

func isXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}
