package compare

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	maxXMLViewSourceBytes = 256 << 10
	maxXMLViewBytes       = 1 << 20
	maxXMLViewLines       = 4000
	xmlNamespace          = "http://www.w3.org/XML/1998/namespace"
)

type xmlViewNode struct {
	name     xml.Name
	attrs    []xml.Attr
	parts    []xmlViewPart
	preserve bool
}

type xmlViewPart struct {
	node *xmlViewNode
	text string
	pi   string
}

func buildXMLView(ctx context.Context, change FileChange, before, after map[string]string, a, b []byte) (*XMLView, error) {
	leftXML := change.Before != nil && change.Before.Kind == "xml"
	rightXML := change.After != nil && change.After.Kind == "xml"
	if !leftXML && !rightXML {
		return nil, nil
	}
	view := &XMLView{Kind: "structure"}
	switch {
	case (leftXML && before == nil) || (rightXML && after == nil):
		view.Kind = "unavailable"
		view.Note = "XML 结构分析不可用，请查看文件诊断及原始摘要。"
		return view, nil
	case change.Before == nil:
		view.Kind = "added"
	case change.After == nil:
		view.Kind = "removed"
	case leftXML && rightXML && change.Before.XMLSHA256 == change.After.XMLSHA256:
		view.Kind = "format"
	}
	if (leftXML && a == nil) || (rightXML && b == nil) {
		view.Note = "XML 源文件超过 256 KiB 对照展示上限，请查看字段差异。"
		return view, nil
	}
	if !leftXML || !rightXML {
		if change.Before != nil && change.After != nil {
			view.Note = "一侧为非 XML 文件，仅显示 XML 一侧的内容。"
		}
	}
	namespaces := map[string]string{}
	left, err := parseXMLView(ctx, a, namespaces)
	if err != nil {
		return xmlViewFailure(ctx, view, err)
	}
	right, err := parseXMLView(ctx, b, namespaces)
	if err != nil {
		return xmlViewFailure(ctx, view, err)
	}
	// 两侧共用命名空间映射，前缀改名不会造成显示内容变化。
	ordered := make([]string, 0, len(namespaces))
	for uri := range namespaces {
		ordered = append(ordered, uri)
	}
	sort.Strings(ordered)
	for i, uri := range ordered {
		namespaces[uri] = fmt.Sprintf("n%d", i+1)
	}
	al, err := formatXMLView(ctx, left, namespaces)
	if err != nil {
		return xmlViewFailure(ctx, view, err)
	}
	bl, err := formatXMLView(ctx, right, namespaces)
	if err != nil {
		return xmlViewFailure(ctx, view, err)
	}
	view.Blocks, view.Coarse, err = alignXMLLines(ctx, al, bl)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func xmlViewFailure(ctx context.Context, view *XMLView, err error) (*XMLView, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	view.Note = "XML 对照不可用：" + err.Error() + "；字段差异仍可查看。"
	return view, nil
}

func parseXMLView(ctx context.Context, data []byte, namespaces map[string]string) (*xmlViewNode, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > maxXMLViewSourceBytes {
		return nil, fmt.Errorf("源文件超过 256 KiB 展示上限")
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	doc := &xmlViewNode{}
	stack := []*xmlViewNode{doc}
	nodes := 0
	remember := func(name xml.Name) {
		if name.Space != "" && name.Space != xmlNamespace {
			namespaces[name.Space] = ""
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		parent := stack[len(stack)-1]
		switch value := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > maxXMLViewLines || len(stack) > 128 {
				return nil, fmt.Errorf("节点数量或深度超过展示上限")
			}
			n := &xmlViewNode{name: value.Name, preserve: parent.preserve || value.Name.Local == "TextCode"}
			remember(value.Name)
			for _, attr := range value.Attr {
				if attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns") {
					continue
				}
				n.attrs = append(n.attrs, attr)
				remember(attr.Name)
				if attr.Name.Space == xmlNamespace && attr.Name.Local == "space" {
					n.preserve = attr.Value == "preserve" || value.Name.Local == "TextCode"
				}
			}
			sort.Slice(n.attrs, func(i, j int) bool { return expanded(n.attrs[i].Name) < expanded(n.attrs[j].Name) })
			parent.parts = append(parent.parts, xmlViewPart{node: n})
			stack = append(stack, n)
		case xml.EndElement:
			meaningful := parent.preserve
			for _, p := range parent.parts {
				if strings.TrimSpace(p.text) != "" {
					meaningful = true
				}
			}
			if !meaningful {
				parts := parent.parts[:0]
				for _, p := range parent.parts {
					if p.node != nil || p.pi != "" {
						parts = append(parts, p)
					}
				}
				parent.parts = parts
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			parent.parts = append(parent.parts, xmlViewPart{text: string(value)})
		case xml.ProcInst:
			if value.Target != "xml" {
				parent.parts = append(parent.parts, xmlViewPart{pi: "<?" + value.Target + " " + string(value.Inst) + "?>"})
			}
		}
	}
	return doc, nil
}

// 字符串按 XML 转义，换行和制表符用字符引用显示，保留文字中的有效空白。
func xmlViewEscape(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return strings.NewReplacer("\n", "&#xA;", "\r", "&#xD;", "\t", "&#x9;").Replace(b.String())
}

func formatXMLView(ctx context.Context, doc *xmlViewNode, namespaces map[string]string) ([]string, error) {
	if doc == nil {
		return nil, nil
	}
	qname := func(n xml.Name) string {
		if n.Space == "" {
			return n.Local
		}
		if n.Space == xmlNamespace {
			return "xml:" + n.Local
		}
		return namespaces[n.Space] + ":" + n.Local
	}
	var lines []string
	budget := &byteBudget{remaining: maxXMLViewBytes, label: "XML 格式化内容"}
	add := func(line string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		parts := strings.Split(line, "\n")
		if len(lines)+len(parts) > maxXMLViewLines {
			return fmt.Errorf("格式化内容超过 4000 行展示上限")
		}
		if err := budget.take(int64(len(line)) + 1); err != nil {
			return err
		}
		lines = append(lines, parts...)
		return nil
	}
	// 只声明本侧实际使用的命名空间，避免另一侧新增命名空间使根行误报变化。
	used := map[string]bool{}
	var collect func(*xmlViewNode)
	collect = func(n *xmlViewNode) {
		if n.name.Space != "" && n.name.Space != xmlNamespace {
			used[n.name.Space] = true
		}
		for _, a := range n.attrs {
			if a.Name.Space != "" && a.Name.Space != xmlNamespace {
				used[a.Name.Space] = true
			}
		}
		for _, p := range n.parts {
			if p.node != nil {
				collect(p.node)
			}
		}
	}
	collect(doc)
	var uris []string
	for uri := range used {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	opening := func(n *xmlViewNode, root bool) string {
		var b strings.Builder
		b.WriteString("<" + qname(n.name))
		if root {
			for _, uri := range uris {
				b.WriteString(" xmlns:" + namespaces[uri] + "=\"" + xmlViewEscape(uri) + "\"")
			}
		}
		for _, a := range n.attrs {
			b.WriteString(" " + qname(a.Name) + "=\"" + xmlViewEscape(a.Value) + "\"")
		}
		if len(n.parts) == 0 {
			b.WriteString("/>")
		} else {
			b.WriteString(">")
		}
		return b.String()
	}
	var inline func(io.Writer, *xmlViewNode, bool) error
	inline = func(w io.Writer, n *xmlViewNode, root bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.WriteString(w, opening(n, root)); err != nil {
			return err
		}
		for _, p := range n.parts {
			if p.node != nil {
				if err := inline(w, p.node, false); err != nil {
					return err
				}
			} else {
				text := p.pi
				if text == "" {
					text = xmlViewEscape(p.text)
				}
				if _, err := io.WriteString(w, text); err != nil {
					return err
				}
			}
		}
		if len(n.parts) > 0 {
			_, err := io.WriteString(w, "</"+qname(n.name)+">")
			return err
		}
		return nil
	}
	var render func(*xmlViewNode, int, bool) error
	render = func(n *xmlViewNode, depth int, root bool) error {
		indent := strings.Repeat("  ", depth)
		meaningful := n.preserve
		for _, p := range n.parts {
			if strings.TrimSpace(p.text) != "" {
				meaningful = true
			}
		}
		if meaningful || len(n.parts) == 0 {
			var b bytes.Buffer
			if err := inline(budgetWriter{&b, &byteBudget{remaining: maxXMLViewBytes, label: "XML 单行内容"}}, n, root); err != nil {
				return err
			}
			return add(indent + b.String())
		}
		if err := add(indent + opening(n, root)); err != nil {
			return err
		}
		for _, p := range n.parts {
			if p.node != nil {
				if err := render(p.node, depth+1, false); err != nil {
					return err
				}
			} else if p.pi != "" {
				if err := add(indent + "  " + p.pi); err != nil {
					return err
				}
			}
		}
		return add(indent + "</" + qname(n.name) + ">")
	}
	for _, p := range doc.parts {
		if p.node != nil {
			if err := render(p.node, 0, true); err != nil {
				return nil, err
			}
		} else if p.pi != "" {
			if err := add(p.pi); err != nil {
				return nil, err
			}
		}
	}
	return lines, ctx.Err()
}

// 为格式化行分配字符编号，复用有界 Myers 搜索进行行级对齐。
func alignXMLLines(ctx context.Context, a, b []string) ([]XMLViewBlock, bool, error) {
	ids := map[string]rune{}
	encode := func(lines []string) string {
		out := make([]rune, len(lines))
		for i, line := range lines {
			id, ok := ids[line]
			if !ok {
				id = rune(len(ids) + 1)
				ids[line] = id
			}
			out[i] = id
		}
		return string(out)
	}
	x, y := encode(a), encode(b)
	spans, coarse, err := textDifferences(ctx, x, y)
	if err != nil {
		return nil, false, err
	}
	if x == y && x != "" {
		spans = []TextSpan{{Kind: "equal", Text: x}}
	}
	var blocks []XMLViewBlock
	ai, bi := 0, 0
	var pendingA, pendingB []int
	flush := func() {
		if len(pendingA)+len(pendingB) == 0 {
			return
		}
		lines := make([]XMLViewLine, max(len(pendingA), len(pendingB)))
		for i, j := range pendingA {
			lines[i].BeforeNumber = j + 1
			lines[i].Before = a[j]
		}
		for i, j := range pendingB {
			lines[i].AfterNumber = j + 1
			lines[i].After = b[j]
		}
		blocks = append(blocks, XMLViewBlock{Lines: lines})
		pendingA, pendingB = nil, nil
	}
	for _, span := range spans {
		n := len([]rune(span.Text))
		switch span.Kind {
		case "delete":
			for range n {
				pendingA = append(pendingA, ai)
				ai++
			}
		case "insert":
			for range n {
				pendingB = append(pendingB, bi)
				bi++
			}
		case "equal":
			flush()
			lines := make([]XMLViewLine, n)
			for i := range lines {
				lines[i] = XMLViewLine{BeforeNumber: ai + 1, AfterNumber: bi + 1, Before: a[ai], After: b[bi]}
				ai++
				bi++
			}
			if n > 8 {
				blocks = append(blocks, XMLViewBlock{Equal: true, Lines: lines[:3]}, XMLViewBlock{Equal: true, Folded: true, Lines: lines[3 : n-3]}, XMLViewBlock{Equal: true, Lines: lines[n-3:]})
			} else {
				blocks = append(blocks, XMLViewBlock{Equal: true, Lines: lines})
			}
		}
	}
	flush()
	return blocks, coarse, ctx.Err()
}
