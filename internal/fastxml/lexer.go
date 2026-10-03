// Package fastxml 提供 OFD 快速解析路径共用的零分配 XML lexer。它包装
// github.com/tdewolff/parse/v2/xml，在原始字节上定位元素区间；低频、结构复杂
// 的子树用 DecodeSpan 截取字节后回退 encoding/xml，语义与 xml.Unmarshal 一致。
//
// 这里只放与具体模型无关的词法工具；页面内容、文档主体等模型重建仍留在
// internal/models，避免为复用 lexer 而导出模型内部方法。
package fastxml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	txml "github.com/tdewolff/parse/v2/xml"
)

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

// StripBOM 去掉 UTF-8 BOM。若数据带 UTF-16 BOM，返回 utf16=true，调用方应回退
// 到 encoding/xml（UTF-16 编码的 OFD 极罕见）。
func StripBOM(data []byte) ([]byte, bool) {
	if bytes.HasPrefix(data, utf8BOM) {
		return data[len(utf8BOM):], false
	}
	if bytes.HasPrefix(data, utf16LEBOM) || bytes.HasPrefix(data, utf16BEBOM) {
		return data, true
	}
	return data, false
}

// Lexer 在原始字节上做 XML 词法分析，token 与区间偏移都指向原始切片，不拷贝。
type Lexer struct {
	lx   *txml.Lexer
	in   *parse.Input
	data []byte
	abs  int
}

// NewLexer 为 data 创建 lexer；data 需在解析期间保持有效。
func NewLexer(data []byte) *Lexer {
	in := parse.NewInputBytes(data)
	return &Lexer{
		lx:   txml.NewLexer(in),
		in:   in,
		data: data,
	}
}

// Next 返回下一个 token，start 是该 token 起始处的绝对字节偏移。绝对偏移直接
// 取自底层 parse.Input 的 Offset()，避免按 token 缓冲长度累加时被词法器内部
// 跳过的空白字节（如开始标签属性之间的空格）所扰动。
func (l *Lexer) Next() (tt txml.TokenType, buf []byte, start int) {
	start = l.in.Offset()
	tt, buf = l.lx.Next()
	l.abs = l.in.Offset()
	return tt, buf, start
}

// Text 返回当前 token 的原始文本（属性名等）。
func (l *Lexer) Text() []byte { return l.lx.Text() }

// AttrVal 返回当前 AttributeToken 的属性值。
func (l *Lexer) AttrVal() []byte { return l.lx.AttrVal() }

// CData 返回当前 CDATA token 的内容。tdewolff 的 xml lexer 在 Next() 中返回的是
// 含 "<![CDATA[" / "]]>" 包装的原始词素，去掉包装的正文在 Text() 里。
func (l *Lexer) CData() []byte { return l.lx.Text() }

// Err 返回词法错误；到达输入末尾时返回 io.EOF。
func (l *Lexer) Err() error {
	if err := l.lx.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("解析 XML 失败: %w", err)
	}
	return io.EOF
}

// Data 返回底层原始字节。
func (l *Lexer) Data() []byte { return l.data }

// SpanElement 从元素起始偏移 start 消费到匹配的结束标记，返回该元素的原始字节
// 区间。调用时该元素的 StartTag token 已被消费。
func (l *Lexer) SpanElement(start int) ([]byte, error) {
	depth := 1
	for {
		tt, _, _ := l.Next()
		switch tt {
		case txml.ErrorToken:
			return nil, l.Err()
		case txml.StartTagToken:
			depth++
		case txml.EndTagToken:
			depth--
			if depth == 0 {
				return l.data[start:l.abs], nil
			}
		case txml.StartTagCloseVoidToken:
			depth--
			if depth == 0 {
				return l.data[start:l.abs], nil
			}
		}
	}
}

// DecodeSpan 捕获 start 起的元素字节区间，交给 encoding/xml 完整解码。
func (l *Lexer) DecodeSpan(start int, dst any) error {
	span, err := l.SpanElement(start)
	if err != nil {
		return err
	}
	if err := xml.NewDecoder(bytes.NewReader(span)).Decode(dst); err != nil {
		name := TargetName(span)
		pre := span
		if len(pre) > 64 {
			pre = pre[:64]
		}
		return fmt.Errorf("解析 XML 失败: 元素 %s 区间 [%d:%d] %q: %w", name, start, start+len(span), pre, err)
	}
	return nil
}

// ReadAttrs 消费开始标签后的属性序列，直到 '>' 或自闭合 '/>'。
func (l *Lexer) ReadAttrs(h func(name string, value []byte) error) (void bool, err error) {
	for {
		tt, _, _ := l.Next()
		switch tt {
		case txml.AttributeToken:
			if err := h(string(LocalName(l.Text())), Unquote(l.AttrVal())); err != nil {
				return false, err
			}
		case txml.StartTagCloseToken:
			return false, nil
		case txml.StartTagCloseVoidToken:
			return true, nil
		case txml.ErrorToken:
			return false, l.Err()
		}
	}
}

// SkipAttrs 跳过开始标签后的全部属性。
func (l *Lexer) SkipAttrs() (bool, error) {
	return l.ReadAttrs(func(string, []byte) error { return nil })
}

// LocalName 去掉可能的命名空间前缀。
func LocalName(b []byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == ':' {
			return b[i+1:]
		}
	}
	return b
}

// EndTagName 从 </Name> 结束标记中取出元素名。
func EndTagName(b []byte) []byte {
	name := b[2 : len(b)-1]
	for len(name) > 0 && (name[len(name)-1] == ' ' || name[len(name)-1] == '\t' || name[len(name)-1] == '\n' || name[len(name)-1] == '\r') {
		name = name[:len(name)-1]
	}
	return LocalName(name)
}

// TargetName 从 <Name ...> 开始标记中取出元素名。
func TargetName(b []byte) []byte {
	s := b[1:]
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '/', '>':
			return LocalName(s[:i])
		}
	}
	return LocalName(s)
}

// Unquote 去掉属性值的引号。
func Unquote(b []byte) []byte {
	if len(b) >= 2 && (b[0] == '"' || b[0] == '\'') && b[len(b)-1] == b[0] {
		return b[1 : len(b)-1]
	}
	return b
}

// Unescape 反解码 XML 文本实体，无实体时零拷贝返回。
func Unescape(b []byte) string {
	if bytes.IndexByte(b, '&') < 0 {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); i++ {
		if b[i] != '&' {
			sb.WriteByte(b[i])
			continue
		}
		j := bytes.IndexByte(b[i:], ';')
		if j < 0 || j > 16 {
			sb.WriteByte(b[i])
			continue
		}
		ent := b[i+1 : i+j]
		if r, ok := xmlEntity(ent); ok {
			sb.WriteRune(r)
		} else {
			sb.Write(b[i : i+j+1])
		}
		i += j
	}
	return sb.String()
}

// xmlEntity 识别 XML 预定义实体与数字字符引用。
func xmlEntity(ent []byte) (rune, bool) {
	switch string(ent) {
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	if len(ent) > 1 && ent[0] == '#' {
		s := ent[1:]
		base := 10
		if len(s) > 0 && (s[0] == 'x' || s[0] == 'X') {
			s = s[1:]
			base = 16
		}
		if v, err := strconv.ParseUint(string(s), base, 32); err == nil {
			return rune(v), true
		}
	}
	return 0, false
}
