package crypt

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/emmansun/gmsm/sm3"
	"github.com/zc310/ofd/pkg/spec"
)

type signatureXML struct {
	SignedInfo struct {
		References struct {
			Method string `xml:"CheckMethod,attr"`
			Items  []struct {
				Path   string `xml:"FileRef,attr"`
				Digest string `xml:"CheckValue"`
			} `xml:"Reference"`
		} `xml:"References"`
	} `xml:"SignedInfo"`
}

type protectedReference struct{ from, target, method, digest string }

type protection struct {
	s          *inspector
	restored   map[string]string
	digests    map[string]map[string][]byte
	references []protectedReference
	affected   map[string]string
}

func newProtection(s *inspector, removed map[string]bool, restored map[string]string) *protection {
	p := &protection{s: s, restored: restored, digests: map[string]map[string][]byte{}, affected: map[string]string{}}
	for name := range removed {
		p.affected[strings.ToLower(name)] = name
	}
	for name := range restored {
		p.affected[strings.ToLower(name)] = name
	}
	return p
}

// 此处检查变换是否改变受保护内容，不能替代 sign.Verify。
// 针对恢复后原始内容的签名可能保留有效性；针对密文、占位文件或被移除的
// 加密元数据的签名需要单独处理。
func (p *protection) observe(name string, data []byte) error {
	if _, ok := p.restored[name]; ok {
		md5sum, sha1sum, sm3sum := md5.Sum(data), sha1.Sum(data), sm3.Sum(data)
		p.digests[name] = map[string][]byte{"MD5": md5sum[:], "SHA1": sha1sum[:], "SM3": sm3sum[:], "1.2.156.10197.1.401": sm3sum[:]}
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	root, err := p.firstRoot(trimmed)
	if name == spec.RootDocument {
		if err != nil || root.Local != "OFD" {
			return fmt.Errorf("%w: 恢复后的 OFD.xml 不是有效主入口", ErrDecryptFailed)
		}
		return p.checkXML(trimmed, "OFD", name)
	}
	if err != nil || root.Local != "Signature" || (root.Space != Namespace && !(p.s.opt.Mode == ModeCompat && root.Space == "")) {
		return nil
	}
	if err := p.checkXML(trimmed, "Signature", name); err != nil {
		return err
	}
	var signature signatureXML
	if err := xml.Unmarshal(trimmed, &signature); err != nil {
		return fmt.Errorf("%w: 无法解析签名引用 %s", ErrProtectedContent, name)
	}
	refs := signature.SignedInfo.References
	method := strings.ToUpper(strings.TrimSpace(refs.Method))
	if method == "" {
		method = "MD5"
	}
	for _, item := range refs.Items {
		resolved, err := signaturePath(name, strings.TrimSpace(item.Path))
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrProtectedContent, name, err)
		}
		// core.Lookup 也支持唯一的大小写不敏感匹配，保护检查必须覆盖现有验签器
		// 通过这种方式解析的引用；恢复计划已拒绝有歧义的物理名称和逻辑名称。
		if affected, ok := p.affected[strings.ToLower(resolved)]; ok {
			p.references = append(p.references, protectedReference{name, affected, method, item.Digest})
		}
	}
	return nil
}

func (p *protection) check() error {
	for _, ref := range p.references {
		if p.restored[ref.target] == "" {
			return fmt.Errorf("%w: %s 保护将被删除的 %s", ErrProtectedContent, ref.from, ref.target)
		}
		actual, ok := p.digests[ref.target][ref.method]
		expected, err := decodeBase64(ref.digest)
		if !ok || err != nil || !bytes.Equal(actual, expected) {
			return fmt.Errorf("%w: %s 对恢复内容 %s 的摘要不匹配或算法不支持", ErrProtectedContent, ref.from, ref.target)
		}
	}
	return nil
}

func (p *protection) firstRoot(data []byte) (xml.Name, error) {
	if len(data) == 0 || data[0] != '<' {
		return xml.Name{}, io.EOF
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		if err := p.s.ctx.Err(); err != nil {
			return xml.Name{}, err
		}
		token, err := decoder.Token()
		if err != nil {
			return xml.Name{}, err
		}
		if start, ok := token.(xml.StartElement); ok {
			return start.Name, nil
		}
	}
}

func (p *protection) checkXML(data []byte, expected, name string) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	nodes, depth, roots := 0, 0, 0
	var children []map[string]bool
	var stack []string
	for {
		if err := p.s.ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %s: XML 解析失败", ErrDecryptFailed, name)
		}
		switch node := token.(type) {
		case xml.StartElement:
			if expected == "Signature" && len(children) > 0 {
				parent := children[len(children)-1]
				if parent[node.Name.Local] && contains([]string{"SignedInfo", "References", "Provider", "SignatureMethod", "SignatureDateTime", "SignedValue", "CheckValue"}, node.Name.Local) {
					return fmt.Errorf("%w: %s: 签名单值字段重复", ErrProtectedContent, name)
				}
				if contains([]string{"SignatureMethod", "SignatureDateTime", "SignedValue", "CheckValue"}, stack[len(stack)-1]) {
					return fmt.Errorf("%w: %s: 签名单值字段包含子元素", ErrProtectedContent, name)
				}
				parent[node.Name.Local] = true
			}
			children = append(children, map[string]bool{})
			stack = append(stack, node.Name.Local)
			seen := map[xml.Name]bool{}
			for _, attr := range node.Attr {
				if seen[attr.Name] {
					return fmt.Errorf("%w: %s: XML 属性重复", ErrProtectedContent, name)
				}
				seen[attr.Name] = true
			}
			nodes++
			depth++
			if nodes > p.s.opt.Limits.MaxXMLNodes || depth > p.s.opt.Limits.MaxXMLDepth {
				return limitError(name, "主入口或签名 XML 超过预算")
			}
			if depth == 1 {
				roots++
				if roots != 1 || node.Name.Local != expected || (node.Name.Space != Namespace && !(p.s.opt.Mode == ModeCompat && node.Name.Space == "")) {
					return fmt.Errorf("%w: %s: XML 根元素或命名空间无效", ErrDecryptFailed, name)
				}
			}
		case xml.EndElement:
			depth--
			children = children[:len(children)-1]
			stack = stack[:len(stack)-1]
		case xml.Directive:
			return fmt.Errorf("%w: %s: 不允许 XML DTD", ErrProtectedContent, name)
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(node)) != "" {
				return fmt.Errorf("%w: %s: 根元素外存在文本", ErrDecryptFailed, name)
			}
		}
	}
	return nil
}

// 签名引用允许在包内路径中使用 '..'，与映射表中的规范绝对路径不同。
// 解析时必须确保路径不越过 ZIP 根目录。
func signaturePath(from, value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\\\x00:") || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("签名引用路径无效")
	}
	var parts []string
	if !strings.HasPrefix(value, "/") {
		if i := strings.LastIndexByte(from, '/'); i >= 0 {
			parts = strings.Split(from[:i], "/")
		}
	}
	for _, segment := range strings.Split(value, "/") {
		switch segment {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", fmt.Errorf("签名引用越过包根目录")
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, segment)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("签名引用指向包根目录")
	}
	return strings.Join(parts, "/"), nil
}
