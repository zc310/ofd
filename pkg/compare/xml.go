package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type xmlFrame struct {
	path     string
	counts   map[string]int
	children []string
	text     strings.Builder
	texts    []string
	preserve bool
}

func expanded(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return "{" + n.Space + "}" + n.Local
}

// xmlFields 忽略前缀、属性顺序、注释和纯元素内容中的缩进。
// TextCode 空白、混合内容、子元素顺序及命名空间 URI 均保留。
func xmlFields(ctx context.Context, data []byte) (map[string]string, error) {
	return xmlFieldsLimit(ctx, data, 32<<20)
}

func xmlFieldsLimit(ctx context.Context, data []byte, limit int64) (map[string]string, error) {
	if limit == 0 {
		limit = 32 << 20
	}
	budget := &byteBudget{remaining: limit, label: "XML 字段表"}
	d := xml.NewDecoder(bytes.NewReader(data))
	fields := map[string]string{}
	put := func(key, value string) error {
		if err := budget.take(int64(len(key)) + int64(len(value)) + 64); err != nil {
			return err
		}
		fields[key] = value
		return nil
	}
	stack := []*xmlFrame{{counts: map[string]int{}}}
	roots, nodes := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		parent := stack[len(stack)-1]
		switch token := t.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 200000 || len(stack) > 128 {
				return nil, fmt.Errorf("XML 结构超过节点或深度限制")
			}
			if len(stack) == 1 {
				roots++
			}
			parent.texts = append(parent.texts, parent.text.String())
			parent.text.Reset()
			name := expanded(token.Name)
			parent.counts[name]++
			parent.children = append(parent.children, name)
			p := parent.path + "/" + name + "[" + strconv.Itoa(parent.counts[name]) + "]"
			f := &xmlFrame{path: p, counts: map[string]int{}, preserve: parent.preserve || token.Name.Local == "TextCode"}
			if err := put(p, "element"); err != nil {
				return nil, err
			}
			for _, attr := range token.Attr {
				if attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns") {
					continue
				}
				key := p + "/@" + expanded(attr.Name)
				if _, exists := fields[key]; exists {
					return nil, fmt.Errorf("XML 元素包含重复属性 %s", expanded(attr.Name))
				}
				if err := put(key, attr.Value); err != nil {
					return nil, err
				}
				if attr.Name.Space == "http://www.w3.org/XML/1998/namespace" && attr.Name.Local == "space" {
					f.preserve = attr.Value == "preserve" || token.Name.Local == "TextCode"
				}
			}
			stack = append(stack, f)
		case xml.CharData:
			if len(stack) == 1 && strings.TrimSpace(string(token)) != "" {
				return nil, fmt.Errorf("XML 根元素之外存在文本")
			}
			parent.text.Write(token)
		case xml.EndElement:
			parent.texts = append(parent.texts, parent.text.String())
			meaningfulText := parent.preserve
			for _, text := range parent.texts {
				if strings.TrimSpace(text) != "" {
					meaningfulText = true
					break
				}
			}
			if len(parent.children) > 0 {
				b, _ := json.Marshal(parent.children)
				if err := put(parent.path+"/#children", string(b)); err != nil {
					return nil, err
				}
			}
			for i, text := range parent.texts {
				if text != "" && meaningfulText {
					if err := put(parent.path+"/#text["+strconv.Itoa(i+1)+"]", text); err != nil {
						return nil, err
					}
				}
			}
			stack = stack[:len(stack)-1]
		case xml.ProcInst:
			if token.Target != "xml" {
				key := "?" + token.Target
				parent.counts[key]++
				if err := put(parent.path+"/"+key+"["+strconv.Itoa(parent.counts[key])+"]", string(token.Inst)); err != nil {
					return nil, err
				}
			}
		}
	}
	if roots != 1 || len(stack) != 1 {
		return nil, fmt.Errorf("XML 必须有一个完整根元素")
	}
	return fields, nil
}

func fieldChanges(before, after map[string]string, limit int) ([]ValueChange, bool) {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	changes := []ValueChange{}
	for _, k := range ordered {
		a, aok := before[k]
		b, bok := after[k]
		if aok == bok && a == b {
			continue
		}
		if len(changes) >= limit {
			return changes, true
		}
		change := ValueChange{Path: k}
		if aok {
			change.Before = &a
		}
		if bok {
			change.After = &b
		}
		changes = append(changes, change)
	}
	return changes, false
}
