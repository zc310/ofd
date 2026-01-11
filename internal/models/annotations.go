package models

import (
	"encoding/xml"
)

// Annotations 注解列表容器
type Annotations struct {
	XMLName xml.Name    `xml:"Annotations"`
	Xmlns   string      `xml:"xmlns,attr"`
	Pages   []AnnotPage `xml:"Page,omitempty"`
}

// AnnotPage 单个页面的注解文件引用
type AnnotPage struct {
	PageID  StRefID `xml:"PageID,attr"`
	FileLoc StLoc   `xml:"FileLoc"`
}
