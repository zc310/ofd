// Package securexml 约束密码元数据 XML 的大小、结构与包内路径。
package securexml

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/zc310/ofd/pkg/spec"
)

const Namespace = spec.Namespace

var ErrLimitExceeded = errors.New("securexml: XML 超过处理预算")

// Decode 在反序列化前检查完整 XML，拒绝歧义单值、DTD、命名空间和预算绕过。
func Decode(ctx context.Context, data []byte, root string, target any) error {
	return DecodeWithLimits(ctx, data, root, target, 100000, 64)
}

// DecodeWithLimits 可收紧硬上限，零值仍使用有限默认值。
func DecodeWithLimits(ctx context.Context, data []byte, root string, target any, maximumNodes, maximumDepth int) error {
	if maximumNodes == 0 || maximumNodes > 100000 {
		maximumNodes = 100000
	}
	if maximumDepth == 0 || maximumDepth > 64 {
		maximumDepth = 64
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("%w: 密码元数据 XML 超过 4 MiB", ErrLimitExceeded)
	}
	d := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	var stack []map[string]bool
	nodes, roots := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch n := token.(type) {
		case xml.StartElement:
			if n.Name.Space != Namespace {
				return fmt.Errorf("XML 命名空间无效: %s", n.Name.Local)
			}
			if len(stack) == 0 {
				roots++
				if roots != 1 || n.Name.Local != root {
					return fmt.Errorf("XML 根元素应为 %s", root)
				}
			} else {
				parent := stack[len(stack)-1]
				repeated := n.Name.Local == "DocBody" || n.Name.Local == "Signature" || n.Name.Local == "Reference" || n.Name.Local == "StampAnnot" || n.Name.Local == "Parameter" || n.Name.Local == "File"
				if parent[n.Name.Local] && !repeated {
					return fmt.Errorf("XML 单值元素重复: %s", n.Name.Local)
				}
				parent[n.Name.Local] = true
			}
			attrs := map[xml.Name]bool{}
			for _, attr := range n.Attr {
				if attrs[attr.Name] {
					return fmt.Errorf("XML 属性重复: %s", attr.Name.Local)
				}
				attrs[attr.Name] = true
				if attr.Name.Space != "" && attr.Name.Space != "xmlns" {
					return fmt.Errorf("密码元数据不允许带命名空间的属性")
				}
			}
			stack = append(stack, map[string]bool{})
			nodes++
			if nodes > maximumNodes || len(stack) > maximumDepth {
				return fmt.Errorf("%w: XML 节点或深度超过上限", ErrLimitExceeded)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.Directive:
			return fmt.Errorf("密码元数据不允许 DTD")
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(n)) != "" {
				return fmt.Errorf("XML 根元素外存在文本")
			}
		}
	}
	if roots != 1 {
		return fmt.Errorf("XML 缺少根元素")
	}
	return xml.Unmarshal(data, target)
}

// Resolve 以引用文件目录为基准，解析可含 .. 的 ST_Loc，禁止越过包根。
func Resolve(from, value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00:") || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("包内路径无效: %q", value)
	}
	var parts []string
	if !strings.HasPrefix(value, "/") {
		if i := strings.LastIndexByte(from, '/'); i >= 0 {
			parts = strings.Split(from[:i], "/")
		}
	}
	for _, p := range strings.Split(value, "/") {
		switch p {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", fmt.Errorf("包内路径越界: %s", value)
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("路径指向包根目录")
	}
	return strings.Join(parts, "/"), nil
}

func ValidID(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r != '_' && !unicode.IsLetter(r) && !(i > 0 && (unicode.IsDigit(r) || r == '.' || r == '-')) {
			return false
		}
	}
	return true
}
