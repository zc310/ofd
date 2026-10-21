package ofdxml

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/beevik/etree"
)

// Marshal 写出修改后的 XML 树，保留原有元素名、命名空间声明和节点顺序。
// 不缩进、不裁剪文本，不修改输入树；输出字节独立于树与写入器缓冲区。
// CDATA 和普通文本统一转义写出。
// XML 声明统一为 UTF-8。与 MarshalDocument 不同，此函数不统一 OFD 前缀，
// 不需要逐节点查找命名空间，适合读取后修改的快速写回。
func Marshal(doc *etree.Document) ([]byte, error) {
	return marshalDocument(doc, false)
}

// MarshalDocument 写出已解析的 XML 树，保留扩展命名空间及节点顺序。
// OFD 元素按 GB/T 33190—2016 §7.1 使用 ofd 前缀，扩展元素使用原名称。
// 对 ofd 已绑定外部命名空间的非规范输入，保留该作用域内 OFD 元素的原名称，
// 避免改变扩展元素、属性或 QName 值的命名空间含义。
// 此函数不修改输入树；返回的字节独立于写入器缓冲区。
func MarshalDocument(doc *etree.Document) ([]byte, error) {
	return marshalDocument(doc, true)
}

func marshalDocument(doc *etree.Document, normalize bool) ([]byte, error) {
	if doc == nil || doc.Root() == nil {
		return nil, fmt.Errorf("XML 缺少根元素")
	}
	if err := validateDocument(doc); err != nil {
		return nil, err
	}
	w := New()
	w.strictXML = true
	for index, token := range doc.Child {
		if text, ok := token.(*etree.CharData); ok && index == 0 && strings.HasPrefix(text.Data, "\ufeff") {
			w.Text(strings.TrimPrefix(text.Data, "\ufeff"))
			continue // 输出已使用 UTF-8 声明，不能把输入 BOM 写到声明之后。
		}
		if instruction, ok := token.(*etree.ProcInst); ok && instruction.Target == "xml" {
			continue // New 已生成 UTF-8 XML 声明。
		}
		if err := writeXMLToken(w, token, normalize, ""); err != nil {
			Put(w)
			return nil, err
		}
	}
	return w.Done(), nil
}

// WriteElement 将元素及其子节点追加到 w，适用于嵌入既有 XML 片段。
// 调用方应在元素上补齐不能从输出父节点继承的命名空间声明。
func WriteElement(w *Writer, element *etree.Element) error {
	if w == nil || element == nil {
		return fmt.Errorf("XML 写入器或元素为空")
	}
	strict := w.strictXML
	w.strictXML = true
	defer func() { w.strictXML = strict }()
	// 父节点可能已经正确提供源子树继承的外部绑定，此时不能用补声明覆盖它。
	for parent := element.Parent(); parent != nil; parent = parent.Parent() {
		for _, attr := range parent.Attr {
			if attr.Space == "xmlns" && attr.Key == prefix {
				if attr.Value != Namespace {
					return writeElement(w, element, true, attr.Value)
				}
				return writeElement(w, element, true, "")
			}
		}
	}
	// 输出父节点的声明未知，子树自行补齐归一化产生的 ofd 前缀。
	return writeElement(w, element, true, "")
}

func writeElement(w *Writer, element *etree.Element, normalize bool, ofdNamespace string) error {
	if w.err != nil {
		return w.err
	}
	if !xmlLocalName(element.Tag) || element.Space != "" && !xmlLocalName(element.Space) {
		return fmt.Errorf("无效的 XML 元素名 %q", element.FullTag())
	}
	for _, attr := range element.Attr {
		if attr.Space == "xmlns" && attr.Key == prefix {
			ofdNamespace = attr.Value
			break
		}
	}
	uri := ofdNamespace
	if normalize || element.Space != "" && element.Space != prefix && element.Space != "xml" {
		uri = element.NamespaceURI()
	}
	if element.Space != "" && element.Space != "xml" && uri == "" {
		return fmt.Errorf("XML 元素 %q 使用了未声明的前缀", element.FullTag())
	}
	// 外部绑定不能覆盖，否则原有 ofd: 属性、扩展节点乃至 QName 值都会改义。
	ofd := normalize && (ofdNamespace == "" || ofdNamespace == Namespace) && uri == Namespace
	declareOFD := ofd && ofdNamespace != Namespace
	if ofd {
		w.startElementName(prefix, element.Tag, false)
	} else {
		w.startElementName(element.Space, element.Tag, false)
	}
	if declareOFD {
		w.Attr("xmlns:ofd", Namespace)
		ofdNamespace = Namespace
	}
	for index, attr := range element.Attr {
		if w.err != nil {
			return w.err
		}
		if !xmlLocalName(attr.Key) || attr.Space != "" && !xmlLocalName(attr.Space) {
			return fmt.Errorf("无效的 XML 属性名 %q", attr.FullKey())
		}
		if attr.Space != "" && attr.Space != "xmlns" && attr.Space != "xml" && namespaceForPrefix(element, attr.Space) == "" {
			return fmt.Errorf("XML 属性 %q 使用了未声明的前缀", attr.FullKey())
		}
		const xmlURI = "http://www.w3.org/XML/1998/namespace"
		const xmlnsURI = "http://www.w3.org/2000/xmlns/"
		if attr.Space == "xmlns" && (attr.Key == "xmlns" || attr.Value == "" || attr.Value == xmlnsURI || (attr.Key == "xml") != (attr.Value == xmlURI)) ||
			attr.Space == "" && attr.Key == "xmlns" && (attr.Value == xmlURI || attr.Value == xmlnsURI) {
			return fmt.Errorf("无效的 XML 命名空间声明 %q", attr.FullKey())
		}
		for _, previous := range element.Attr[:index] {
			if previous.Key == attr.Key && (previous.Space == attr.Space || previous.Space != "" && attr.Space != "" && previous.Space != "xmlns" && attr.Space != "xmlns" && namespaceForPrefix(element, previous.Space) == namespaceForPrefix(element, attr.Space)) {
				return fmt.Errorf("XML 属性 %q 重复", attr.FullKey())
			}
		}
		if declareOFD && attr.Space == "xmlns" && attr.Key == prefix {
			continue // 补声明时避免重复输出原有的空绑定。
		}
		w.startAttrName(attr.Space, attr.Key)
		w.appendEscaped(attr.Value, false)
		w.buf = append(w.buf, '"')
	}
	for _, token := range element.Child {
		if err := writeXMLToken(w, token, normalize, ofdNamespace); err != nil {
			return err
		}
	}
	w.End()
	return w.err
}

func writeXMLToken(w *Writer, token etree.Token, normalize bool, ofdNamespace string) error {
	if w.err != nil {
		return w.err
	}
	switch node := token.(type) {
	case *etree.Element:
		return writeElement(w, node, normalize, ofdNamespace)
	case *etree.CharData:
		w.Text(node.Data)
	case *etree.Comment:
		if strings.Contains(node.Data, "--") || strings.HasSuffix(node.Data, "-") || !validXMLText(node.Data) {
			return fmt.Errorf("无效的 XML 注释")
		}
		w.closeStart()
		w.appendString("<!--")
		w.appendString(node.Data)
		w.appendString("-->")
	case *etree.Directive:
		return fmt.Errorf("OFD XML 写回不支持 XML 指令")
	case *etree.ProcInst:
		if strings.EqualFold(node.Target, "xml") {
			return fmt.Errorf("XML 声明只能出现在文档开头")
		}
		if !xmlName([]byte(node.Target), false) || strings.Contains(node.Inst, "?>") || !validXMLText(node.Inst) {
			return fmt.Errorf("无效的 XML 处理指令")
		}
		w.closeStart()
		w.appendString("<?")
		w.appendString(node.Target)
		if node.Inst != "" {
			w.appendString(" ")
			w.appendString(node.Inst)
		}
		w.appendString("?>")
	default:
		return fmt.Errorf("无法写出 XML 节点 %T", token)
	}
	return w.err
}

func namespaceForPrefix(element *etree.Element, space string) string {
	if space == "xml" {
		return "http://www.w3.org/XML/1998/namespace"
	}
	for parent := element; parent != nil; parent = parent.Parent() {
		for _, attr := range parent.Attr {
			if attr.Space == "xmlns" && attr.Key == space {
				return attr.Value
			}
		}
	}
	return ""
}

func xmlLocalName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] >= utf8.RuneSelf {
		return !strings.ContainsRune(name, ':') && xmlName([]byte(name), false)
	}
	if xmlLocalNameChars[name[0]]&1 == 0 {
		return false
	}
	for i := 1; i < len(name); i++ {
		if xmlLocalNameChars[name[i]]&2 == 0 {
			return name[i] >= utf8.RuneSelf && !strings.ContainsRune(name, ':') && xmlName([]byte(name), false)
		}
	}
	return true
}

// OFD 的名称均为 ASCII；表查询避免重复走 Unicode 名称范围判断。
var xmlLocalNameChars = func() (table [256]byte) {
	for c := byte('a'); c <= 'z'; c++ {
		table[c], table[c-'a'+'A'] = 3, 3
	}
	table['_'] = 3
	for c := byte('0'); c <= '9'; c++ {
		table[c] = 2
	}
	table['-'], table['.'] = 2, 2
	return
}()

func validXMLText(text string) bool {
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 1 || !isXMLChar(r) {
			return false
		}
		text = text[size:]
	}
	return true
}

func validateDocument(doc *etree.Document) error {
	roots, declarationAllowed := 0, true
	for index, token := range doc.Child {
		switch node := token.(type) {
		case *etree.Element:
			roots++
		case *etree.CharData:
			text := node.Data
			if index == 0 {
				text = strings.TrimPrefix(text, "\ufeff")
				if text == "" && node.Data == "\ufeff" && !node.IsCData() {
					continue
				}
			}
			if node.IsCData() || !xmlWhitespace([]byte(text)) {
				return fmt.Errorf("XML 根元素之外不能有文本或 CDATA")
			}
		case *etree.ProcInst:
			if strings.EqualFold(node.Target, "xml") && (!declarationAllowed || node.Target != "xml") {
				return fmt.Errorf("XML 声明只能出现在文档开头一次")
			}
		}
		declarationAllowed = false
	}
	if roots != 1 {
		return fmt.Errorf("XML 必须且只能有一个根元素")
	}
	return nil
}
