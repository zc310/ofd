package crypt

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/zc310/ofd/pkg/spec"
)

type scopeNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr  `xml:",any,attr"`
	Children []scopeNode `xml:",any"`
	Text     string      `xml:",chardata"`
}

func (n *scopeNode) attr(name string) string {
	for _, attr := range n.Attrs {
		if attr.Name.Local == name && attr.Name.Space == "" {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func (n *scopeNode) children(name string) []*scopeNode {
	var out []*scopeNode
	for i := range n.Children {
		if n.Children[i].XMLName.Local == name {
			out = append(out, &n.Children[i])
		}
	}
	return out
}

func (n *scopeNode) walk(visit func(*scopeNode)) {
	visit(n)
	for i := range n.Children {
		n.Children[i].walk(visit)
	}
}

// pageEntries 按 GM/T 0099 C.1、GB/T 33190 §7.4–7.9、§15、§18 扩展范围。
// 资源文件不按 ID 拆分：整个资源索引和所列数据纳入范围，覆盖复合对象的间接引用。
func (s *inspector) pageEntries(selection PageSelection) ([]string, error) {
	selected := map[string]bool{}
	cache := map[string]*scopeNode{}
	read := func(name, root string) (*scopeNode, error) {
		if cached := cache[name]; cached != nil {
			if cached.XMLName.Local != root {
				return nil, invalid(name, "范围引用的 XML 类型冲突")
			}
			return cached, nil
		}
		data, err := s.read(name)
		if err != nil {
			return nil, err
		}
		defer clear(data)
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		guard := newProtection(s, nil, nil)
		if err := guard.checkXML(data, root, name); err != nil {
			return nil, fmt.Errorf("%w: %s: 页面范围 XML 无效: %w", ErrInvalidMetadata, name, err)
		}
		var node scopeNode
		if err := xml.Unmarshal(data, &node); err != nil {
			return nil, invalid(name, "页面范围 XML 无法解析: %v", err)
		}
		valid := true
		node.walk(func(n *scopeNode) {
			// 999.ofd 的注释参数省略命名空间；参数值不定义文件引用，
			// 整份注释仍被选中并原样加密。其他外来命名空间不作引用推断。
			parameter := root == "PageAnnot" && n.XMLName.Space == "" && (n.XMLName.Local == "Parameters" || n.XMLName.Local == "Parameter")
			valid = valid && (n.XMLName.Space == Namespace || parameter)
		})
		if !valid {
			return nil, invalid(name, "页面范围包含不支持的 XML 命名空间")
		}
		cache[name] = &node
		return &node, nil
	}
	resolve := func(from, value string) (string, error) {
		name, err := signaturePath(from, strings.TrimSpace(value))
		if err != nil {
			return "", invalid(from, "页面范围路径无效: %v", err)
		}
		entry, ok := s.files[name]
		if !ok || entry.IsDir {
			return "", invalid(name, "页面范围引用缺失或指向目录")
		}
		return name, nil
	}
	root, err := read(spec.RootDocument, "OFD")
	if err != nil {
		return nil, err
	}
	var body *scopeNode
	for _, candidate := range root.children("DocBody") {
		for _, doc := range candidate.children("DocRoot") {
			name, err := resolve(spec.RootDocument, doc.Text)
			if err != nil {
				return nil, err
			}
			if name == selection.Document {
				if body != nil {
					return nil, invalid(name, "页面范围的文档入口不唯一")
				}
				body = candidate
			}
		}
	}
	if body == nil {
		return nil, invalid(selection.Document, "所选文档未被 OFD.xml 引用")
	}
	doc, err := read(selection.Document, "Document")
	if err != nil {
		return nil, err
	}
	templates := map[string]string{}
	var resources []string
	for _, common := range doc.children("CommonData") {
		for _, node := range common.children("TemplatePage") {
			id := node.attr("ID")
			if id == "" || templates[id] != "" {
				return nil, invalid(selection.Document, "模板 ID 缺失或重复")
			}
			name, err := resolve(selection.Document, node.attr("BaseLoc"))
			if err != nil {
				return nil, err
			}
			templates[id] = name
		}
		for _, kind := range []string{"PublicRes", "DocumentRes"} {
			for _, node := range common.children(kind) {
				name, err := resolve(selection.Document, node.Text)
				if err != nil {
					return nil, err
				}
				resources = append(resources, name)
			}
		}
	}
	resourcesDone := map[string]bool{}
	addResource := func(name string) error {
		if resourcesDone[name] {
			return nil
		}
		res, err := read(name, "Res")
		if err != nil {
			return err
		}
		selected[name] = true
		resourcesDone[name] = true
		base := name
		if loc := res.attr("BaseLoc"); loc != "" {
			base, err = signaturePath(name, strings.TrimRight(loc, "/")+"/_resource")
			if err != nil {
				return invalid(name, "资源 BaseLoc 越界或无效")
			}
		}
		var firstErr error
		res.walk(func(node *scopeNode) {
			if firstErr != nil {
				return
			}
			var value string
			switch node.XMLName.Local {
			case "FontFile", "MediaFile":
				value = strings.TrimSpace(node.Text)
				if value == "" {
					firstErr = invalid(name, "资源数据路径为空")
					return
				}
			case "ColorSpace":
				value = node.attr("Profile")
			}
			if value != "" {
				target, err := resolve(base, value)
				if err != nil {
					firstErr = err
					return
				}
				selected[target] = true
			}
		})
		return firstErr
	}
	for _, resource := range resources {
		if err := addResource(resource); err != nil {
			return nil, err
		}
	}
	visiting := map[string]bool{}
	pagesDone := map[string]bool{}
	var addPage func(string, string) error
	addPage = func(name, kind string) error {
		if len(visiting) >= s.opt.Limits.MaxXMLDepth {
			return limitError(name, "模板依赖深度超过上限")
		}
		if visiting[name] {
			return invalid(name, "模板引用存在循环")
		}
		if pagesDone[name] {
			return nil
		}
		page, err := read(name, kind)
		if err != nil {
			return err
		}
		selected[name], visiting[name] = true, true
		defer delete(visiting, name)
		for _, node := range page.children("PageRes") {
			target, err := resolve(name, node.Text)
			if err != nil {
				return err
			}
			if err := addResource(target); err != nil {
				return err
			}
		}
		for _, node := range page.children("Template") {
			target := templates[node.attr("TemplateID")]
			if target == "" {
				return invalid(name, "页面引用未知模板")
			}
			if err := addPage(target, "Page"); err != nil {
				return err
			}
		}
		pagesDone[name] = true
		return nil
	}
	pages := map[string]string{}
	for _, list := range doc.children("Pages") {
		for _, page := range list.children("Page") {
			id := page.attr("ID")
			if id == "" || pages[id] != "" {
				return nil, invalid(selection.Document, "页面 ID 缺失或重复")
			}
			name, err := resolve(selection.Document, page.attr("BaseLoc"))
			if err != nil {
				return nil, err
			}
			pages[id] = name
		}
	}
	ids := map[string]bool{}
	for _, id := range selection.IDs {
		if pages[id] == "" {
			return nil, invalid(selection.Document, "所选页面 ID 不存在: %s", id)
		}
		ids[id] = true
		if err := addPage(pages[id], "Page"); err != nil {
			return nil, err
		}
	}
	for _, node := range doc.children("Annotations") {
		name, err := resolve(selection.Document, node.Text)
		if err != nil {
			return nil, err
		}
		annotations, err := read(name, "Annotations")
		if err != nil {
			return nil, err
		}
		for _, page := range annotations.children("Page") {
			if !ids[page.attr("PageID")] {
				continue
			}
			for _, loc := range page.children("FileLoc") {
				target, err := resolve(name, loc.Text)
				if err != nil {
					return nil, err
				}
				if err := addPage(target, "PageAnnot"); err != nil {
					return nil, err
				}
			}
		}
	}
	// 印章外观位于签名 XML/签名值中，不能只加密普通 PageAnnot。
	for _, loc := range body.children("Signatures") {
		name, err := resolve(spec.RootDocument, loc.Text)
		if err != nil {
			return nil, err
		}
		list, err := read(name, "Signatures")
		if err != nil {
			return nil, err
		}
		for _, signature := range list.children("Signature") {
			target, err := resolve(name, signature.attr("BaseLoc"))
			if err != nil {
				return nil, err
			}
			sig, err := read(target, "Signature")
			if err != nil {
				return nil, err
			}
			matches := false
			sig.walk(func(node *scopeNode) {
				matches = matches || node.XMLName.Local == "StampAnnot" && ids[node.attr("PageRef")]
			})
			if !matches {
				continue
			}
			selected[target] = true
			var firstErr error
			sig.walk(func(node *scopeNode) {
				if firstErr != nil {
					return
				}
				for _, child := range node.Children {
					if child.XMLName.Local == "SignedValue" || node.XMLName.Local == "Seal" && (child.XMLName.Local == "BaseLoc" || child.XMLName.Local == "ImageLoc") {
						entry, err := resolve(target, child.Text)
						if err != nil {
							firstErr = err
							return
						}
						selected[entry] = true
					}
				}
			})
			if firstErr != nil {
				return nil, firstErr
			}
		}
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
