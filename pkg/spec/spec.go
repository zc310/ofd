// Package spec 定义 OFD 标准数据模型、基础类型和固定常量，供解析、创建、校验
// 及外部使用者共享。本包不负责文档加载、渲染或业务权限执行。
// 结构模型以声明和 XML 解码为主，完整文档创建使用 pkg/creator；基础值已有的
// XML 编解码能力不代表 xml.Marshal 可以生成完整标准文档。
package spec

import "slices"

// Namespace 是标准 OFD 命名空间。
const Namespace = "http://www.ofdspec.org/2016"

// RootDocument 是 OFD 包根文档的文件名。
const RootDocument = "OFD.xml"

// GM/T 0099—2020 定义的密码应用入口，均位于 OFD 包根目录。
const (
	// EncryptionEntryPoint 是附录 C.3 的解密入口文件名。
	EncryptionEntryPoint = "Encryptions.xml"
	// IntegrityEntryPoint 是附录 D.2 的完整性保护描述文件名。
	IntegrityEntryPoint = "OFDEntries.xml"
)

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
	// 限制扩大到全部文档编辑功能。
	DocTypeOFDH = "OFD-H"
)

// DocTypes 是全部已知 DocType 取值，按 profile 从通用到专用排列。
var DocTypes = []string{DocTypeOFD, DocTypeOFDA, DocTypeOFDH}

// IsDocType 判断 value 是否为已知 DocType 取值。
func IsDocType(value string) bool {
	return slices.Contains(DocTypes, value)
}
