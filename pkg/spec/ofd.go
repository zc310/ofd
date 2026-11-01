package spec

import (
	"encoding/xml"
)

// OFD 是 GB/T 33190—2016 §7.4 表3的主入口，不包含包加载状态。
type OFD struct {
	// XMLName 文档根元素名称。
	XMLName xml.Name `xml:"OFD"` //nolint:tagliatelle // 标准根元素为大写OFD。
	// XMLNS OFD 命名空间地址。
	XMLNS string `xml:"xmlns:ofd,attr"`
	// Version OFD 文档规范版本。
	Version string `xml:"Version,attr"`
	// DocType 文档类型。
	DocType string `xml:"DocType,attr"`
	// DocBodies 文档体列表。
	DocBodies []DocBody `xml:"DocBody"`
}

// DocBody OFD 文档体。
type DocBody struct {
	// DocInfo 文档元数据信息。
	DocInfo DocInfo `xml:"DocInfo"`
	// DocRoot 文档根目录的位置。
	DocRoot StLoc `xml:"DocRoot"`
	// Versions 文档版本集合。
	Versions *Versions `xml:"Versions,omitempty"`
	// Signatures 签名文件的位置。
	Signatures *StLoc `xml:"Signatures,omitempty"`
}

// Versions 是§19.1表70的版本入口集合。
type Versions struct {
	// Version 文档版本列表。
	VersionList []Version `xml:"Version"`
}

// Version 文档版本定义。
type Version struct {
	// ID 文档版本标识。
	ID string `xml:"ID,attr"` //nolint:tagliatelle // OFD标准属性名为大写ID。
	// Index 文档版本序号。
	Index int `xml:"Index,attr"`
	// Current 是否为当前使用的版本。
	Current bool `xml:"Current,attr"`
	// BaseLoc 版本文件的基础路径。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// DocInfo 是§7.4表4的文档元数据，保留可选字段的缺省状态。
type DocInfo struct {
	// DocID 文档标识。
	DocID string `xml:"DocID"` //nolint:tagliatelle // OFD标准元素名为DocID。
	// Title 文档标题。
	Title *string `xml:"Title,omitempty"`
	// Author 文档作者。
	Author *string `xml:"Author,omitempty"`
	// Subject 文档主题。
	Subject *string `xml:"Subject,omitempty"`
	// Abstract 文档摘要。
	Abstract *string `xml:"Abstract,omitempty"`
	// CreationDate 文档创建时间。
	CreationDate *DateTime `xml:"CreationDate,omitempty"`
	// ModDate 文档最后修改时间。
	ModDate *DateTime `xml:"ModDate,omitempty"`
	// DocUsage 文档用途。
	DocUsage *string `xml:"DocUsage,omitempty"`
	// Cover 文档封面文件的位置。
	Cover *StLoc `xml:"Cover,omitempty"`
	// Keywords 文档关键词集合。
	Keywords *Keywords `xml:"Keywords,omitempty"`
	// Creator 创建文档的软件名称。
	Creator *string `xml:"Creator,omitempty"`
	// CreatorVersion 创建文档的软件版本。
	CreatorVersion *string `xml:"CreatorVersion,omitempty"`
	// CustomDatas 文档自定义数据集合。
	CustomDatas *CustomDatas `xml:"CustomDatas,omitempty"`
}

// Keywords 文档关键词集合。
type Keywords struct {
	// Keyword 关键词列表。
	Keyword []string `xml:"Keyword"`
}

// CustomDatas 是表4的自定义元数据集合，保留条目顺序及重复名称。
type CustomDatas struct {
	CustomData []CustomData `xml:"CustomData"`
}

// CustomData 保存一项自定义元数据的名称及文本。
type CustomData struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}
