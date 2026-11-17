package spec

import (
	"encoding/xml"
)

// Attachments 是 GB/T 33190—2016 §20.1 表72的附件列表。
type Attachments struct {
	XMLName     xml.Name     `xml:"Attachments"`
	Xmlns       string       `xml:"xmlns,attr,omitempty"` //nolint:tagliatelle // XML 命名空间属性固定为小写 xmlns。
	Attachments []Attachment `xml:"Attachment,omitempty"`
}

// Attachment 是 §20.2 表73的附件声明，不包含附件内容及加载状态。
type Attachment struct {
	ID           string   `xml:"ID,attr"` //nolint:tagliatelle // OFD标准属性名为大写ID。
	Name         string   `xml:"Name,attr"`
	Format       *string  `xml:"Format,attr,omitempty"`
	CreationDate DateTime `xml:"CreationDate,attr,omitempty"`
	ModDate      DateTime `xml:"ModDate,attr,omitempty"`
	Size         *float64 `xml:"Size,attr,omitempty"`
	Visible      Bool     `xml:"Visible,attr,omitempty"`
	Usage        string   `xml:"Usage,attr,omitempty"`
	FileLoc      StLoc    `xml:"FileLoc"`
}
