package spec

import (
	"encoding/xml"
	"fmt"
)

// PageAnnot 是 GB/T 33190—2016 §15 表61的分页注释文件，不包含资源加载状态。
type PageAnnot struct {
	// XMLName 页面注解根元素名称。
	XMLName xml.Name `xml:"PageAnnot"`
	// Xmlns 页面注解命名空间地址。
	Xmlns string `xml:"xmlns,attr"` //nolint:tagliatelle // XML 命名空间属性固定为小写 xmlns。
	// Annots 页面中的注解列表。
	Annots []*Annot `xml:"Annot"`
}

// Annot 单个页面注解定义。
type Annot struct {
	// ID 注解标识，在页面注解文件内唯一。
	ID string `xml:"ID,attr"` //nolint:tagliatelle // OFD 标准属性名为 ID。
	// Type 注解类型。
	Type AnnotType `xml:"Type,attr"`
	// Creator 创建该注解的软件或用户名称。
	Creator string `xml:"Creator,attr"`
	// LastModDate 注解最后修改时间。
	LastModDate DateTime `xml:"LastModDate,attr"`
	// Visible 表示该注释对象是否显示，未指定时默认为 true。
	Visible Bool `xml:"Visible,attr,omitempty"`
	// Subtype 注解子类型。
	Subtype string `xml:"Subtype,attr,omitempty"`
	// Print 是否随页面打印 Remark 信息，缺省为 true；不控制 Appearance（§15 表61）。
	Print Bool `xml:"Print,attr,omitempty"`
	// NoZoom 是否在缩放页面时保持 Remark 显示大小不变。
	NoZoom bool `xml:"NoZoom,attr,omitempty"`
	// NoRotate 是否在旋转页面时保持 Remark 显示方向不变。
	NoRotate bool `xml:"NoRotate,attr,omitempty"`
	// ReadOnly 是否禁止用户更改 Remark，未指定时按规范默认为 true。
	ReadOnly Bool `xml:"ReadOnly,attr,omitempty"`
	// Remark 注解备注。
	Remark *string `xml:"Remark,omitempty"`
	// Parameters 注解参数集合。
	Parameters *Params `xml:"Parameters,omitempty"`
	// Appearance 注解外观内容。
	Appearance *Appearance `xml:"Appearance"`
}

// AnnotType 注解类型枚举。
type AnnotType string

const (
	// AnnotTypeLink 表示链接注解。
	AnnotTypeLink AnnotType = "Link"
	// AnnotTypePath 表示路径注解。
	AnnotTypePath AnnotType = "Path"
	// AnnotTypeHighlight 表示高亮注解。
	AnnotTypeHighlight AnnotType = "Highlight"
	// AnnotTypeStamp 表示图章注解。
	AnnotTypeStamp AnnotType = "Stamp"
	// AnnotTypeWatermark 表示水印注解。
	AnnotTypeWatermark AnnotType = "Watermark"
)

// Params 注解参数集合。
type Params struct {
	// Parameters 注解参数列表。
	Parameters []Parameter `xml:"Parameter"`
}

// Parameter 单个注解参数。
type Parameter struct {
	// Name 参数名称。
	Name string `xml:"Name,attr"`
	// Value 参数值。
	Value string `xml:",chardata"`
}

// Appearance 注解外观定义。
type Appearance struct {
	// CTPageBlock 注解外观中的页面对象。
	CTPageBlock

	// Boundary 注解外观的边界框。
	Boundary *StBox `xml:"Boundary,attr,omitempty"`
}

// UnmarshalXML 先解析外观边界，再将其内容交由共享的页面块解析器处理。
func (a *Appearance) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		if attr.Name.Local == "Boundary" {
			var boundary StBox
			if err := boundary.UnmarshalXMLAttr(attr); err != nil {
				return err
			}
			a.Boundary = &boundary
		}
	}
	return a.CTPageBlock.UnmarshalXML(d, start)
}

// UnmarshalXMLAttr 从 XML 属性中解析并校验注解类型。
//
//nolint:err113 // 迁移时保留既有解码错误文本。
func (a *AnnotType) UnmarshalXMLAttr(attr xml.Attr) error {
	switch attr.Value {
	case "Link", "Path", "Highlight", "Stamp", "Watermark":
		*a = AnnotType(attr.Value)
		return nil
	default:
		return fmt.Errorf("无效的注解类型: %s", attr.Value)
	}
}
