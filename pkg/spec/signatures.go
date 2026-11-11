package spec

import "encoding/xml"

// Signatures 是 GB/T 33190—2016 §18.1 表66的签名索引，不包含已加载的签名描述。
type Signatures struct {
	XMLName    xml.Name       `xml:"Signatures"`
	Xmlns      string         `xml:"xmlns,attr"` //nolint:tagliatelle // XML 命名空间属性固定为小写 xmlns。
	MaxSignID  *string        `xml:"MaxSignId,omitempty"`
	Signatures []SignatureRef `xml:"Signature,omitempty"`
}

// SignatureRef 保存签名的索引属性；Relative 是 GM/T 0099 的基础签名关联扩展。
// ID 和 Relative 均保留字符串原文，不将 xs:ID 当作数字 ST_ID 处理。
type SignatureRef struct {
	ID       string `xml:"ID,attr"` //nolint:tagliatelle // OFD 标准属性名为 ID。
	BaseLoc  StLoc  `xml:"BaseLoc,attr"`
	Type     string `xml:"Type,attr"`
	Relative string `xml:"Relative,attr"`
}
