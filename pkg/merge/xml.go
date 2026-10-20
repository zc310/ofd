package merge

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/ofdxml"
)

// marshalOFDXML 由 ofdxml 写出修改后的 XML，保留扩展命名空间及节点顺序。
// OFD 元素按 GB/T 33190—2016 §7.1 使用 ofd 前缀，扩展元素使用原名称。
func marshalOFDXML(doc *etree.Document) ([]byte, error) {
	if doc.Root() == nil {
		return nil, fmt.Errorf("XML 缺少根元素")
	}
	prefixOFDXML(doc.Root())
	w := ofdxml.New()
	defer ofdxml.Put(w)
	for index, token := range doc.Child {
		if text, ok := token.(*etree.CharData); ok && index == 0 && strings.HasPrefix(text.Data, "\ufeff") {
			w.Text(strings.TrimPrefix(text.Data, "\ufeff"))
			continue // 输出已使用 UTF-8 声明，不能把输入 BOM 写到声明之后。
		}
		if instruction, ok := token.(*etree.ProcInst); ok && instruction.Target == "xml" {
			continue // New 已生成 UTF-8 XML 声明。
		}
		if err := writeXMLToken(w, token, true); err != nil {
			return nil, err
		}
	}
	return bytes.Clone(w.Bytes()), nil
}

func writeXMLElement(w *ofdxml.Writer, element *etree.Element, root bool) error {
	if element.Space == "ofd" {
		w.Start(element.Tag)
	} else {
		w.StartQualified(element.FullTag())
	}
	for _, attr := range element.Attr {
		if root && element.Space == "ofd" && attr.Space == "xmlns" && attr.Key == "ofd" {
			continue // Start 已声明根元素的 ofd 命名空间。
		}
		w.Attr(attr.FullKey(), attr.Value)
	}
	for _, token := range element.Child {
		if err := writeXMLToken(w, token, false); err != nil {
			return err
		}
	}
	w.End()
	return nil
}

func writeXMLToken(w *ofdxml.Writer, token etree.Token, root bool) error {
	switch node := token.(type) {
	case *etree.Element:
		return writeXMLElement(w, node, root)
	case *etree.CharData:
		if node.IsCData() {
			w.Raw([]byte("<![CDATA[" + strings.ReplaceAll(node.Data, "]]>", "]]]]><![CDATA[>") + "]]>"))
		} else {
			w.Text(node.Data)
		}
	case *etree.Comment:
		w.Raw([]byte("<!--" + node.Data + "-->"))
	case *etree.Directive:
		w.Directive(node.Data)
	case *etree.ProcInst:
		if strings.EqualFold(node.Target, "xml") {
			return fmt.Errorf("XML 声明只能出现在文档开头")
		}
		instruction := "<?" + node.Target
		if node.Inst != "" {
			instruction += " " + node.Inst
		}
		w.Raw([]byte(instruction + "?>"))
	default:
		return fmt.Errorf("无法写出 XML 节点 %T", token)
	}
	return nil
}
