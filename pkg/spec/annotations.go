package spec

import (
	"encoding/xml"
)

// Annotations 是 GB/T 33190—2016 §15.1 表60的注释索引，不包含注释外观。
type Annotations struct {
	XMLName xml.Name    `xml:"Annotations"`
	Xmlns   string      `xml:"xmlns,attr,omitempty"` //nolint:tagliatelle // XML 命名空间属性固定为小写 xmlns。
	Pages   []AnnotPage `xml:"Page,omitempty"`
}

// AnnotPage 保存单个页面的分页注释文件引用。
type AnnotPage struct {
	PageID  StRefID `xml:"PageID,attr"` //nolint:tagliatelle // OFD标准属性名为PageID。
	FileLoc StLoc   `xml:"FileLoc"`
}
