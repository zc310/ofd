package models

import (
	"encoding/xml"
)

// OFD OFD 文档根元素。
type OFD struct {
	// XMLName 文档根元素名称。
	XMLName xml.Name `xml:"OFD"`
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

// Versions 文档版本集合。
type Versions struct {
	// Version 文档版本列表。
	VersionList []Version `xml:"Version"`
}

// Version 文档版本定义。
type Version struct {
	// ID 文档版本标识。
	ID string `xml:"ID,attr"`
	// Index 文档版本序号。
	Index int `xml:"Index,attr"`
	// Current 是否为当前使用的版本。
	Current bool `xml:"Current,attr"`
	// BaseLoc 版本文件的基础路径。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// DocInfo 文档元数据信息。
type DocInfo struct {
	// DocID 文档标识。
	DocID string `xml:"DocID"`
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

// String 返回 OFD 文档的 XML 字符串表示。
func (p *OFD) String() string {
	//p.XMLNS = "http://www.ofdspec.org/2016"

	buf, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(buf)
}
