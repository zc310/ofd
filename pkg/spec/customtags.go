package spec

import "encoding/xml"

// CustomTags 是 GB/T 33190—2016 §16 表63及附录A模式的自定义标引列表。
type CustomTags struct {
	XMLName    xml.Name    `xml:"CustomTags"`
	Xmlns      string      `xml:"xmlns,attr,omitempty"` //nolint:tagliatelle // XML 命名空间属性固定为小写 xmlns。
	CustomTags []CustomTag `xml:"CustomTag,omitempty"`
}

// CustomTag 是自定义标引入口，资源内容及定位结果由加载方处理。
type CustomTag struct {
	// NameSpace 按附录A的 CustomTags.xsd 声明标引命名空间。
	NameSpace string `xml:"NameSpace,attr"`
	// TypeID 保留表63中的类型标识，兼容既有生产者写法。
	TypeID    string `xml:"TypeID,attr,omitempty"` //nolint:tagliatelle // 表63属性名为 TypeID。
	SchemaLoc *StLoc `xml:"SchemaLoc,omitempty"`
	FileLoc   StLoc  `xml:"FileLoc"`
}
