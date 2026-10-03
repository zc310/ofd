// Package spec 定义 OFD 规范的固定常量，供解析、创建、校验等各包共享，也供
// 外部使用者构造和校验 OFD 文档时引用，避免在各处硬编码字符串。
package spec

import "slices"

// Namespace 是标准 OFD 命名空间。
const Namespace = "http://www.ofdspec.org/2016"

// RootDocument 是 OFD 包根文档的文件名。
const RootDocument = "OFD.xml"

// DocType 是 OFD.xml 根节点 DocType 属性的取值，用于声明文档遵循的
// profile。标准正文本身不校验该属性的具体取值，而是由各领域应用规范
// 收紧，因此取值随 profile 变化。
const (
	// DocTypeOFD 是基础 OFD，取 GB/T 33190 的一般版式文档。
	DocTypeOFD = "OFD"
	// DocTypeOFDA 是档案长期保存 profile，取 GB/T 42133—2022《信息技术
	// OFD档案应用指南》6.2.1 a)：该标准按「禁用清单」限制 OFD 特性以满足
	// DA/T 47 的长期保存要求，并规定阅读软件见到该取值时应禁用插入页面、
	// 调整页面等文档编辑功能。
	DocTypeOFDA = "OFD-A"
	// DocTypeOFDH 是电子病历版式文档 profile。该取值出自
	// GB/T 48666-2026《电子病历版式文档技术要求》7.1 a)，该标准在
	// GB/T 33190 与 GB/T 42133 的基础上叠加医疗领域要求，并把阅读软件的
	// 限制扩大到全部文档编辑功能。该标准目前为征求意见稿，条款与取值可能
	// 变化。
	DocTypeOFDH = "OFD-H"
)

// DocTypes 是全部已知 DocType 取值，按 profile 从通用到专用排列。
var DocTypes = []string{DocTypeOFD, DocTypeOFDA, DocTypeOFDH}

// IsDocType 判断 value 是否为已知 DocType 取值。
func IsDocType(value string) bool {
	return slices.Contains(DocTypes, value)
}
