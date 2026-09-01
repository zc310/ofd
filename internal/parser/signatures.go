package parser

import (
	"encoding/asn1"
	"fmt"
	"io"
	"os"
	"strings"
)

// supportedTypes 支持的签章文件类型。
var supportedTypes = map[string]bool{
	"png":  true,
	"ofd":  true,
	"jpg":  true,
	"jpeg": true,
	"bmp":  true,
}

// SealData 存储提取的签章数据。
type SealData struct {
	FileType string // 文件类型: png, ofd, jpg, jpeg
	Data     []byte // 提取的字节数据
}

// ExtractSealData 从签章数据中提取文件类型与内容，支持文件路径(string)、字节数据([]byte)
// 或 io.Reader 输入。
func ExtractSealData(source interface{}) (*SealData, error) {
	var data []byte
	var err error
	switch src := source.(type) {
	case string:
		data, err = os.ReadFile(src)
	case []byte:
		data = src
	case io.Reader:
		data, err = io.ReadAll(src)
	default:
		return nil, fmt.Errorf("不支持的源类型: %T", source)
	}
	if err != nil {
		return nil, err
	}

	// 解析 ASN.1 根节点。
	var root asn1.RawValue
	if _, err := asn1.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("解析ASN.1失败: %w", err)
	}

	// 深度优先遍历所有节点，查找 4 元素 SEQUENCE：[IA5String, OCTET STRING, INTEGER, INTEGER]。
	stack := []*asn1.RawValue{&root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if isSequence(node) {
			children := parseChildren(node.Bytes)
			if hasValidType(children) {
				if sealData := extractSealData(children); sealData != nil {
					return sealData, nil
				}
			}
		}

		// 复合类型继续向下遍历子节点。
		if isCompositeType(node) {
			children := parseChildren(node.Bytes)
			for i := len(children) - 1; i >= 0; i-- {
				stack = append(stack, &children[i])
			}
		}
	}

	return nil, fmt.Errorf("未找到有效的签章数据")
}

// parseChildren 解析节点的所有子元素。
func parseChildren(data []byte) []asn1.RawValue {
	var children []asn1.RawValue
	rest := data
	for len(rest) > 0 {
		var child asn1.RawValue
		r, err := asn1.Unmarshal(rest, &child)
		if err != nil {
			break
		}
		rest = r
		children = append(children, child)
	}
	return children
}

// hasValidType 检查 4 元素类型是否为 [IA5String, OCTET STRING, INTEGER, INTEGER]。
func hasValidType(children []asn1.RawValue) bool {
	return len(children) == 4 &&
		isIA5String(&children[0]) &&
		isOctetString(&children[1]) &&
		isInteger(&children[2]) &&
		isInteger(&children[3])
}

// extractSealData 从 4 元素 SEQUENCE 中提取签章数据。
func extractSealData(children []asn1.RawValue) *SealData {
	fileType := ia5StringValue(&children[0])
	if fileType == "" {
		return nil
	}
	fileType = strings.ToLower(fileType)
	if !supportedTypes[fileType] {
		return nil
	}
	octetData, err := octetStringValue(&children[1])
	if err != nil {
		return nil
	}
	return &SealData{FileType: fileType, Data: octetData}
}

// isSequence 判断是否为 SEQUENCE 类型。
func isSequence(node *asn1.RawValue) bool {
	return node != nil && node.Class == asn1.ClassUniversal && node.Tag == asn1.TagSequence
}

// isCompositeType 判断是否为复合类型（SEQUENCE/SET）。
func isCompositeType(node *asn1.RawValue) bool {
	return node != nil && node.Class == asn1.ClassUniversal &&
		(node.Tag == asn1.TagSequence || node.Tag == asn1.TagSet)
}

// isIA5String 判断是否为 IA5String 类型。
func isIA5String(node *asn1.RawValue) bool {
	return node != nil && node.Class == asn1.ClassUniversal && node.Tag == asn1.TagIA5String
}

// isOctetString 判断是否为 OctetString 类型。
func isOctetString(node *asn1.RawValue) bool {
	return node != nil && node.Class == asn1.ClassUniversal && node.Tag == asn1.TagOctetString
}

// isInteger 判断是否为 INTEGER 类型。
func isInteger(node *asn1.RawValue) bool {
	return node != nil && node.Class == asn1.ClassUniversal && node.Tag == asn1.TagInteger
}

// ia5StringValue 获取 IA5String 的值。
func ia5StringValue(node *asn1.RawValue) string {
	if !isIA5String(node) {
		return ""
	}
	var value string
	if _, err := asn1.Unmarshal(node.FullBytes, &value); err != nil {
		return ""
	}
	return value
}

// octetStringValue 获取 OctetString 的值。
func octetStringValue(node *asn1.RawValue) ([]byte, error) {
	if !isOctetString(node) {
		return nil, fmt.Errorf("不是OctetString类型")
	}
	var data []byte
	_, err := asn1.Unmarshal(node.FullBytes, &data)
	return data, err
}
