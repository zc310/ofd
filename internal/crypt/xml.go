package crypt

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/zc310/ofd/internal/core"
)

type encryptionXML struct {
	Operations []operationXML `xml:"EncryptInfo"`
}

type operationXML struct {
	ID       string `xml:"ID,attr"`
	Relative string `xml:"Relative,attr"`
	Provider struct {
		Name    string `xml:"Name,attr"`
		Company string `xml:"Company,attr"`
		Version string `xml:"Version,attr"`
	} `xml:"Provider"`
	Scope          string `xml:"EncryptScope"`
	EncryptDate    string `xml:"EncryptDate"`
	DecryptSeedLoc string `xml:"DecryptSeedLoc"`
	EntriesMapLoc  string `xml:"EntriesMapLoc"`
}

type seedXML struct {
	ID           string    `xml:"ID,attr"`
	Scheme       string    `xml:"EncryptCaseId,attr"`
	Users        []userXML `xml:"UserInfo"`
	ExtendParams *struct{} `xml:"ExtendParams"`
}

type userXML struct {
	Name        string  `xml:"UserName,attr"`
	Type        string  `xml:"UserType,attr"`
	Certificate string  `xml:"UserCert"`
	WrappedKey  string  `xml:"EncryptedWK"`
	IV          *string `xml:"IVValue"`
}

type entriesXML struct {
	ID      string `xml:"ID,attr"`
	Entries []struct {
		Path  string `xml:"Path,attr"`
		EPath string `xml:"EPath,attr"`
		Seed  string `xml:"DecryptSeedLoc,attr"`
	} `xml:"EncryptEntry"`
}

// packagePath 接受 C.4 要求的包内绝对路径，不接受宿主文件系统的绝对路径。
// 相对路径以引用文件所在位置为基准解析。
func packagePath(from, value string, absolute bool) (string, error) {
	if value == "" || strings.ContainsAny(value, "\\\x00:") || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("非法包内路径 %q", value)
	}
	if absolute && !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("映射路径应为包内绝对路径: %q", value)
	}
	name := strings.TrimPrefix(value, "/")
	if err := core.ValidateEntryName(name); err != nil {
		return "", err
	}
	if !strings.HasPrefix(value, "/") {
		if i := strings.LastIndexByte(from, '/'); i >= 0 {
			name = from[:i+1] + name
		}
	}
	return name, nil
}

// decodeXML 在反序列化前检查整份文档，防止被忽略的子树绕过深度、节点数、
// 命名空间或文档尾部内容检查。
func (s *inspector) decodeXML(data []byte, file string, roots []string, target any) error {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, nodes, rootCount := 0, 0, 0
	missingNS := false
	var children []map[string]bool
	for {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return invalid(file, "XML 解析失败: %v", err)
		}
		switch node := token.(type) {
		case xml.StartElement:
			if len(children) > 0 {
				parent := children[len(children)-1]
				if parent[node.Name.Local] && !contains([]string{"EncryptInfo", "UserInfo", "EncryptEntry", "Parameter"}, node.Name.Local) {
					return invalid(file, "单值元素重复: %s", node.Name.Local)
				}
				parent[node.Name.Local] = true
			}
			children = append(children, map[string]bool{})
			depth++
			nodes++
			if depth > s.opt.Limits.MaxXMLDepth || nodes > s.opt.Limits.MaxXMLNodes {
				return limitError(file, "XML 节点数或深度超过上限")
			}
			if depth == 1 {
				rootCount++
				if rootCount != 1 || !contains(roots, node.Name.Local) {
					return invalid(file, "根元素应为 %s", strings.Join(roots, "/"))
				}
				if node.Name.Local != roots[0] {
					s.diag("encryption.root_alias", file, "采用标准图表中的另一根元素拼写: "+node.Name.Local)
				}
			}
			if node.Name.Space != Namespace {
				if s.opt.Mode != ModeCompat || node.Name.Space != "" {
					return invalid(file, "命名空间无效: %q", node.Name.Space)
				}
				missingNS = true
			}
			seen := make(map[xml.Name]bool, len(node.Attr))
			for _, attr := range node.Attr {
				if seen[attr.Name] {
					return invalid(file, "XML 属性重复: %s", attr.Name.Local)
				}
				seen[attr.Name] = true
				if attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns") {
					continue
				}
				if attr.Name.Space != "" {
					return invalid(file, "属性使用了命名空间: %s", attr.Name.Local)
				}
			}
		case xml.EndElement:
			depth--
			children = children[:len(children)-1]
		case xml.Directive:
			return invalid(file, "不允许 XML DTD 或指令声明")
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(node)) != "" {
				return invalid(file, "根元素外存在文本")
			}
		}
	}
	if rootCount != 1 {
		return invalid(file, "缺少根元素")
	}
	if missingNS {
		s.diag("encryption.namespace_missing", file, "兼容读取缺少 OFD 命名空间的加密元数据")
	}
	if err := xml.Unmarshal(data, target); err != nil {
		return invalid(file, "XML 解析失败: %v", err)
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
