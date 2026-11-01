package models

import (
	"encoding/xml"

	"github.com/zc310/ofd/pkg/spec"
)

// Annotations 注解列表容器
type Annotations struct {
	XMLName xml.Name    `xml:"Annotations"`
	Xmlns   string      `xml:"xmlns,attr"`
	Pages   []AnnotPage `xml:"Page,omitempty"`
}

// AnnotPage 单个页面的注解文件引用
type AnnotPage struct {
	PageID  StRefID    `xml:"PageID,attr"` //nolint:tagliatelle // OFD标准属性名为PageID。
	FileLoc spec.StLoc `xml:"FileLoc"`
}
