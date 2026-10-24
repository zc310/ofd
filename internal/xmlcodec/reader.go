package xmlcodec

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/beevik/etree"
	"github.com/tdewolff/parse/v2"
	txml "github.com/tdewolff/parse/v2/xml"
)

// Parse 读取 OFD 包中的 UTF-8 XML 1.0 文档，返回可用 etree API 修改的树。
// 直接通过 lexer 构树，不调用 etree 的解析器。保留文本空白、扩展命名空间、
// 注释和处理指令；CDATA 作为普通文本读取。按 XML 1.0 归一化字面换行和属性空白，
// 保留字符引用值。
// 支持预定义实体和数字引用，不接受 DOCTYPE 或自定义实体。
// 树不依赖输入切片的生命周期。跨文档移动子树须由调用方补齐命名空间声明。
func Parse(data []byte) (*etree.Document, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) || containsNonXMLRune(string(data)) {
		return nil, fmt.Errorf("解析 XML 失败: 输入不是合法的 UTF-8 XML 字符")
	}
	for _, c := range data {
		if c < 0x20 && !xmlSpace(c) {
			return nil, fmt.Errorf("解析 XML 失败: 非法控制字符 0x%02x", c)
		}
	}
	// lexer 会改写属性空白，并在尾部放置哨兵。只复制一次，属性值仍从
	// 原始 data 取值，先合并 CRLF，再归一化属性空白，避免把 CRLF 变为两个空格。
	buffer := make([]byte, len(data)+1)
	copy(buffer, data)
	in := parse.NewInputBytes(buffer[:len(data)])
	lex := txml.NewLexer(in)
	doc := etree.NewDocument()
	parent := &doc.Element
	open, rootSeen := false, false
	depth := 0
	fail := func(message string) (*etree.Document, error) {
		return nil, fmt.Errorf("解析 XML 失败（字节 %d）: %s", in.Offset(), message)
	}
	// 按文档驻留名称，只在新名称出现时检查 Unicode 名称规则。
	names := make(map[string]string)
	nameString := func(raw []byte) (string, bool) {
		if name, ok := names[string(raw)]; ok {
			return name, true
		}
		if !xmlName(raw, true) {
			return "", false
		}
		name := string(raw)
		names[name] = name
		return name, true
	}
	for {
		start := in.Offset()
		// lexer 将 PI 正文当属性读取，因此声明和处理指令在进入 lexer 前
		// 扫描。其余 OFD 元素、属性、文本、注释及 CDATA 直接使用 lexer。
		if !open && start < len(data) && data[start] == '<' {
			rest := data[start:]
			if bytes.HasPrefix(rest, []byte("<?")) {
				target, instruction, size, ok := readProcInst(rest)
				if !ok {
					return fail("无效或未结束的处理指令")
				}
				if strings.EqualFold(target, "xml") {
					if target != "xml" || start != 0 || !xmlDeclaration(instruction) {
						return fail("无效的 XML 声明；只支持 UTF-8 XML 1.0")
					}
				}
				parent.CreateProcInst(target, instruction)
				in.Move(size)
				in.Skip()
				continue
			}
			if bytes.HasPrefix(rest, []byte("<!DOCTYPE")) {
				return fail("OFD XML 读取不支持 DOCTYPE")
			}
		}
		tt, raw := lex.Next()
		switch tt {
		case txml.StartTagToken:
			name, ok := nameString(lex.Text())
			if open || !ok {
				return fail("无效的元素开始标签")
			}
			if depth == 1024 {
				return nil, fmt.Errorf("解析 XML 失败: %w", etree.ErrMaxDepth)
			}
			if depth == 0 {
				if rootSeen {
					return fail("只能有一个根元素")
				}
				rootSeen = true
			}
			parent = parent.CreateElement(name)
			depth++
			open = true
		case txml.AttributeToken:
			value := lex.AttrVal()
			name, ok := nameString(lex.Text())
			if !open || !ok || len(value) < 2 ||
				(value[0] != '"' && value[0] != '\'') || value[len(value)-1] != value[0] ||
				start >= len(data) || !xmlSpace(data[start]) {
				return fail("无效的属性或属性之间缺少空白")
			}
			value = data[in.Offset()-len(value)+1 : in.Offset()-1]
			if bytes.IndexByte(value, '<') >= 0 {
				return fail("属性中包含未转义的 <")
			}
			text, ok := xmlAttribute(value)
			if !ok {
				return fail("属性中包含无效的字符引用")
			}
			count := len(parent.Attr)
			parent.CreateAttr(name, text)
			if len(parent.Attr) == count {
				return fail("属性名称重复")
			}
		case txml.StartTagCloseToken, txml.StartTagCloseVoidToken:
			if !open {
				return fail("开始标签之外出现标签结束标记")
			}
			open = false
			if tt == txml.StartTagCloseVoidToken {
				parent = parent.Parent()
				depth--
			}
		case txml.EndTagToken:
			if open || depth == 0 || !bytes.HasSuffix(raw, []byte(">")) {
				return fail("无效的元素结束标签")
			}
			name, ok := nameString(lex.Text())
			if !ok {
				return fail("结束标签名称无效")
			}
			space, tag, prefixed := strings.Cut(name, ":")
			if !prefixed {
				tag, space = space, ""
			}
			if parent.Tag != tag || parent.Space != space {
				return fail("元素的开始与结束标签不匹配")
			}
			parent = parent.Parent()
			depth--
		case txml.TextToken:
			if open || bytes.Contains(raw, []byte("]]>")) || depth == 0 && !xmlWhitespace(raw) {
				return fail("无效的文本或根元素之外出现文本")
			}
			text, ok := xmlText(raw)
			if !ok {
				return fail("文本中包含无效的字符引用")
			}
			parent.CreateText(text)
		case txml.CommentToken:
			if open || len(raw) < 7 || !bytes.HasSuffix(raw, []byte("-->")) || bytes.Contains(raw[4:len(raw)-3], []byte("--")) || len(raw) > 7 && raw[len(raw)-4] == '-' {
				return fail("无效或未结束的注释")
			}
			parent.CreateComment(xmlLines(lex.Text()))
		case txml.CDATAToken:
			if open || depth == 0 || len(raw) < 12 || !bytes.HasSuffix(raw, []byte("]]>")) {
				return fail("无效或未结束的 CDATA")
			}
			parent.CreateText(xmlLines(lex.Text()))
		case txml.ErrorToken:
			if lex.Err() != io.EOF || open || depth != 0 || !rootSeen {
				return fail("文档不完整或缺少根元素")
			}
			return doc, nil
		default:
			return fail("意外的 XML 标记")
		}
	}
}

func xmlSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func xmlWhitespace(text []byte) bool {
	for _, c := range text {
		if !xmlSpace(c) {
			return false
		}
	}
	return true
}

// xmlName 遵循 XML 1.0 第五版 §2.3 的名称字符范围；qname 要求至多一个
// 冒号，且前缀和本地名都非空。ASCII 名称不需要 Unicode 解码。
func xmlName(name []byte, qname bool) bool {
	first, colon := true, false
	for len(name) > 0 {
		r, size := rune(name[0]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRune(name)
			if r == utf8.RuneError && size == 1 {
				return false
			}
		}
		name = name[size:]
		if r == ':' {
			if qname {
				if first || colon {
					return false
				}
				first, colon = true, true
				continue
			}
		} else if !xmlNameStart(r) && (first || !(r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xB7 || r >= 0x300 && r <= 0x36F || r >= 0x203F && r <= 0x2040)) {
			return false
		}
		first = false
	}
	return !first
}

func xmlNameStart(r rune) bool {
	return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' ||
		r >= 0xC0 && r <= 0xD6 || r >= 0xD8 && r <= 0xF6 || r >= 0xF8 && r <= 0x2FF ||
		r >= 0x370 && r <= 0x37D || r >= 0x37F && r <= 0x1FFF || r >= 0x200C && r <= 0x200D ||
		r >= 0x2070 && r <= 0x218F || r >= 0x2C00 && r <= 0x2FEF || r >= 0x3001 && r <= 0xD7FF ||
		r >= 0xF900 && r <= 0xFDCF || r >= 0xFDF0 && r <= 0xFFFD || r >= 0x10000 && r <= 0xEFFFF
}
