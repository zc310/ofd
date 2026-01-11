package parser

import (
	"encoding/asn1"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// SealData 存储提取的签章数据
type SealData struct {
	FileType string // 文件类型: png, ofd, jpg, jpeg
	Data     []byte // 提取的字节数据
}

// ASN1Extractor 用于从ASN.1结构中提取数据的提取器
type ASN1Extractor struct {
	Data *SealData
}

// NewASN1Extractor 创建新的ASN1提取器
func NewASN1Extractor() *ASN1Extractor {

	return &ASN1Extractor{}
}

// ExtractFromFile 从文件中提取数据
func (e *ASN1Extractor) ExtractFromFile(filename string) error {
	// 读取文件
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("读取文件失败: %v", err)
	}

	e.Data = nil
	return e.extractFromBytes(data)
}

// ExtractFromBytes 从字节数据中提取
func (e *ASN1Extractor) ExtractFromBytes(data []byte) error {
	e.Data = nil
	return e.extractFromBytes(data)
}

// ExtractFromReader 从 io.Reader 中提取数据
func (e *ASN1Extractor) ExtractFromReader(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("读取数据失败: %w", err)
	}
	return e.ExtractFromBytes(data)
}

// GetSealData 获取提取的签章数据
func (e *ASN1Extractor) GetSealData() *SealData {
	return e.Data
}

// extractFromBytes 内部提取方法
func (e *ASN1Extractor) extractFromBytes(data []byte) error {
	// 解析ASN.1根节点
	var root asn1.RawValue
	rest, err := asn1.Unmarshal(data, &root)
	if err != nil {
		return fmt.Errorf("解析ASN.1失败: %v", err)
	}

	if len(rest) > 0 {
		slog.Debug(fmt.Sprintf("有%d字节未解析", len(rest)))
	}

	// 使用迭代方式搜索
	e.searchNodeIterative(&root)

	if e.Data == nil {
		return fmt.Errorf("未找到有效的签章数据")
	}

	return nil
}

// 搜索节点信息结构
type nodeSearchInfo struct {
	node   *asn1.RawValue
	parent *asn1.RawValue
}

// searchNodeIterative 迭代方式搜索节点
func (e *ASN1Extractor) searchNodeIterative(root *asn1.RawValue) {
	stack := []nodeSearchInfo{{
		node:   root,
		parent: nil,
	}}

	for len(stack) > 0 {
		// 弹出栈顶元素
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		node := current.node

		// 检查是否为SEQUENCE类型
		if e.isSequence(node) {
			// 解析SEQUENCE的子元素
			children := e.parseChildren(node.Bytes)

			// 检查是否为4元素SEQUENCE
			if len(children) == 4 {

				// 检查4元素类型是否符合要求
				if e.check4ElementTypes(children) {
					// 提取数据
					if e.extractFromValidSequence(children) {
						return // 成功提取则返回
					}
				}
			}
		}

		// 如果是复合类型，继续递归处理子节点
		if e.isCompositeType(node) {
			// 解析子节点
			children := e.parseChildren(node.Bytes)

			// 逆序压栈以保持深度优先遍历顺序
			for i := len(children) - 1; i >= 0; i-- {
				child := &children[i]

				stack = append(stack, nodeSearchInfo{
					node:   child,
					parent: node, // 当前节点作为父节点

				})
			}
		}
	}
}

// check4ElementTypes 检查4元素SEQUENCE的类型是否符合要求
// 要求：IA5String, OCTET STRING, INTEGER, INTEGER
func (e *ASN1Extractor) check4ElementTypes(children []asn1.RawValue) bool {
	if len(children) != 4 {
		return false
	}

	// 检查第一个元素：IA5String
	if !e.isIA5String(&children[0]) {
		return false
	}

	// 检查第二个元素：OCTET STRING
	if !e.isOctetString(&children[1]) {
		return false
	}

	// 检查第三个元素：INTEGER
	if !e.isInteger(&children[2]) {
		return false
	}

	// 检查第四个元素：INTEGER
	if !e.isInteger(&children[3]) {
		return false
	}

	return true
}

// extractFromValidSequence 从有效的4元素序列中提取数据
func (e *ASN1Extractor) extractFromValidSequence(children []asn1.RawValue) bool {
	// 第一个元素：IA5String，检查文件类型
	fileType := e.getIA5StringValue(&children[0])
	if fileType == "" {
		return false
	}

	// 检查文件类型是否为支持的格式
	supportedTypes := map[string]bool{
		"png":  true,
		"ofd":  true,
		"jpg":  true,
		"jpeg": true,
	}

	fileTypeLower := strings.ToLower(fileType)
	if !supportedTypes[fileTypeLower] {
		return false
	}

	// 第二个元素：OCTET STRING，提取数据
	octetData, err := e.getOctetStringValue(&children[1])
	if err != nil {
		return false
	}

	// 检查文件大小限制（50MB）
	if len(octetData) > 50*1024*1024 {
		return false
	}

	// 成功提取数据
	e.Data = &SealData{
		FileType: fileTypeLower,
		Data:     octetData,
	}

	return true
}

// 辅助函数

// parseChildren 解析节点的所有子元素
func (e *ASN1Extractor) parseChildren(data []byte) []asn1.RawValue {
	var children []asn1.RawValue
	rest := data

	for len(rest) > 0 {
		var child asn1.RawValue
		var err error
		rest, err = asn1.Unmarshal(rest, &child)
		if err != nil {
			break
		}
		children = append(children, child)
	}

	return children
}

// isCompositeType 判断是否为复合类型
func (e *ASN1Extractor) isCompositeType(node *asn1.RawValue) bool {
	return node != nil &&
		node.Class == asn1.ClassUniversal &&
		(node.Tag == asn1.TagSequence || node.Tag == asn1.TagSet)
}

// isSequence 判断是否为SEQUENCE类型
func (e *ASN1Extractor) isSequence(node *asn1.RawValue) bool {
	return node != nil &&
		node.Class == asn1.ClassUniversal &&
		node.Tag == asn1.TagSequence
}

// isIA5String 判断是否为IA5String类型
func (e *ASN1Extractor) isIA5String(node *asn1.RawValue) bool {
	return node != nil &&
		node.Class == asn1.ClassUniversal &&
		node.Tag == asn1.TagIA5String
}

// isOctetString 判断是否为OctetString类型
func (e *ASN1Extractor) isOctetString(node *asn1.RawValue) bool {
	return node != nil &&
		node.Class == asn1.ClassUniversal &&
		node.Tag == asn1.TagOctetString
}

// isInteger 判断是否为INTEGER类型
func (e *ASN1Extractor) isInteger(node *asn1.RawValue) bool {
	return node != nil &&
		node.Class == asn1.ClassUniversal &&
		node.Tag == asn1.TagInteger
}

// getIA5StringValue 获取IA5String的值
func (e *ASN1Extractor) getIA5StringValue(node *asn1.RawValue) string {
	if !e.isIA5String(node) {
		return ""
	}

	var value string
	if _, err := asn1.Unmarshal(node.FullBytes, &value); err != nil {
		return ""
	}
	return value
}

// getOctetStringValue 获取OctetString的值
func (e *ASN1Extractor) getOctetStringValue(node *asn1.RawValue) ([]byte, error) {
	if !e.isOctetString(node) {
		return nil, fmt.Errorf("不是OctetString类型")
	}

	var data []byte
	_, err := asn1.Unmarshal(node.FullBytes, &data)
	return data, err
}

// getIntegerValue 获取INTEGER的值
func (e *ASN1Extractor) getIntegerValue(node *asn1.RawValue) (int, error) {
	if !e.isInteger(node) {
		return 0, fmt.Errorf("不是INTEGER类型")
	}

	var value int
	_, err := asn1.Unmarshal(node.FullBytes, &value)
	return value, err
}

// getParentTagName 获取父节点类型名称
func (e *ASN1Extractor) getParentTagName(parent *asn1.RawValue) string {
	if parent == nil {
		return "nil"
	}
	return e.getTagName(parent.Tag)
}

// getTagName 获取标签名称
func (e *ASN1Extractor) getTagName(tag int) string {
	switch tag {
	case asn1.TagSequence:
		return "SEQUENCE"
	case asn1.TagSet:
		return "SET"
	case asn1.TagIA5String:
		return "IA5String"
	case asn1.TagOctetString:
		return "OCTET STRING"
	case asn1.TagInteger:
		return "INTEGER"
	case asn1.TagOID:
		return "OBJECT IDENTIFIER"
	case asn1.TagUTCTime:
		return "UTCTime"
	case asn1.TagGeneralizedTime:
		return "GeneralizedTime"
	case asn1.TagUTF8String:
		return "UTF8String"
	case asn1.TagPrintableString:
		return "PrintableString"
	default:
		return fmt.Sprintf("Unknown(%d)", tag)
	}
}

// 公开接口函数

// ExtractSealData 通用提取函数，支持多种输入
func ExtractSealData(source interface{}) (*SealData, error) {
	extractor := NewASN1Extractor()

	var err error
	switch src := source.(type) {
	case string:
		err = extractor.ExtractFromFile(src)
	case []byte:
		err = extractor.ExtractFromBytes(src)
	case io.Reader:
		err = extractor.ExtractFromReader(src)
	default:
		return nil, fmt.Errorf("不支持的源类型: %T", source)
	}

	if err != nil {
		return nil, err
	}

	return extractor.GetSealData(), nil
}

// FindAllValidSequences 查找所有有效的4元素SEQUENCE
func (e *ASN1Extractor) FindAllValidSequences(data []byte) ([]*SealData, error) {
	// 解析ASN.1根节点
	var root asn1.RawValue
	_, err := asn1.Unmarshal(data, &root)
	if err != nil {
		return nil, fmt.Errorf("解析ASN.1失败: %v", err)
	}

	// 查找所有有效的SEQUENCE
	var results []*SealData
	stack := []nodeSearchInfo{{
		node:   &root,
		parent: nil,
	}}

	for len(stack) > 0 {
		// 弹出栈顶元素
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		node := current.node

		// 检查是否为SEQUENCE类型
		if e.isSequence(node) {
			// 解析SEQUENCE的子元素
			children := e.parseChildren(node.Bytes)

			// 检查是否为4元素SEQUENCE
			if len(children) == 4 {
				// 检查4元素类型是否符合要求
				if e.check4ElementTypes(children) {
					// 尝试提取数据
					if sealData := e.tryExtractFromSequence(children); sealData != nil {
						results = append(results, sealData)
					}
				}
			}
		}

		// 如果是复合类型，继续递归处理子节点
		if e.isCompositeType(node) {
			// 解析子节点
			children := e.parseChildren(node.Bytes)

			// 逆序压栈以保持深度优先遍历顺序
			for i := len(children) - 1; i >= 0; i-- {
				child := &children[i]

				stack = append(stack, nodeSearchInfo{
					node:   child,
					parent: node,
				})
			}
		}
	}

	return results, nil
}

// tryExtractFromSequence 尝试从序列中提取数据（不设置e.Data）
func (e *ASN1Extractor) tryExtractFromSequence(children []asn1.RawValue) *SealData {
	// 第一个元素：IA5String，检查文件类型
	fileType := e.getIA5StringValue(&children[0])
	if fileType == "" {
		return nil
	}

	// 检查文件类型是否为支持的格式
	supportedTypes := map[string]bool{
		"png":  true,
		"ofd":  true,
		"jpg":  true,
		"jpeg": true,
	}

	fileTypeLower := strings.ToLower(fileType)
	if !supportedTypes[fileTypeLower] {
		return nil
	}

	// 第二个元素：OCTET STRING，提取数据
	octetData, err := e.getOctetStringValue(&children[1])
	if err != nil {
		return nil
	}

	// 检查文件大小限制（50MB）
	if len(octetData) > 50*1024*1024 {
		return nil
	}

	return &SealData{
		FileType: fileTypeLower,
		Data:     octetData,
	}
}
