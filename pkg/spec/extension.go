package spec

import (
	"encoding/xml"
	"time"
)

// Extensions 是 GB/T 33190—2016 §17 表64的扩展信息列表，不加载扩展数据文件。
type Extensions struct {
	XMLName    xml.Name    `xml:"Extensions"`
	Xmlns      string      `xml:"xmlns,attr"` //nolint:tagliatelle // XML 命名空间属性固定为小写 xmlns。
	Extensions []Extension `xml:"Extension"`
}

// Extension 是 §17 表65的扩展信息，保留属性、内联数据和外部数据路径。
type Extension struct {
	AppName    string  `xml:"AppName,attr"`
	Company    *string `xml:"Company,attr,omitempty"`
	AppVersion *string `xml:"AppVersion,attr,omitempty"`
	// Date 沿用 RFC3339 日期时间解析；缺省或不支持的日期文本保持 nil。
	Date       *time.Time          `xml:"Date,attr,omitempty"`
	RefID      StRefID             `xml:"RefId,attr"`
	Properties []ExtensionProperty `xml:"Property,omitempty"`
	// Data 为 nil 表示省略；非 nil 的空值表示存在空 Data 元素。
	Data       *ExtensionData `xml:"Data,omitempty"`
	ExtendData *StLoc         `xml:"ExtendData,omitempty"`
}

// ExtensionProperty 是扩展的名称、类型和文本值，不解释厂商自定义类型。
type ExtensionProperty struct {
	Name  string  `xml:"Name,attr"`
	Type  *string `xml:"Type,attr,omitempty"`
	Value string  `xml:",chardata"`
}

// ExtensionData 保存内联 XML 扩展子内容，包括 §17 的 xs:anyType 和密码方案扩展参数，
// 不解释或执行其中的 XML。
// InnerXML 保留原始子内容，包括混合文本、注释和处理指令；前缀可能引用祖先命名空间，
// 不能脱离原始命名空间上下文直接当作独立 XML 文档使用，也不是完整文档编码接口。
type ExtensionData struct {
	// Attrs 保存扩展元素自身的属性，包括在该元素上声明的命名空间。
	Attrs []xml.Attr `xml:",any,attr"`
	// InnerXML 保存扩展元素的原始内部 XML，不包含元素自身的起止标签。
	InnerXML string `xml:",innerxml"` //nolint:tagliatelle // innerxml 是 encoding/xml 的固定选项，字段使用 XML 惯用缩写。
	// Text 保存扩展元素的直接字符数据（含 CDATA），不包含后代元素中的文本。
	Text string `xml:",chardata"`
}

// UnmarshalXML 解码扩展声明，只读取直接子元素；未知子树整体跳过。
func (e *Extension) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*e = Extension{}
	// 解析属性
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "AppName":
			e.AppName = attr.Value
		case "Company":
			val := attr.Value
			e.Company = &val
		case "AppVersion":
			val := attr.Value
			e.AppVersion = &val
		case "Date":
			if t, err := time.Parse(time.RFC3339, attr.Value); err == nil {
				e.Date = &t
			}
		case "RefId":
			var id StID
			_ = id.UnmarshalText([]byte(attr.Value))
			e.RefID = StRefID(id)
		}
	}

	// 解析子元素
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}

		switch elem := token.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "Property":
				var prop ExtensionProperty
				if err := d.DecodeElement(&prop, &elem); err != nil {
					return err
				}
				e.Properties = append(e.Properties, prop)
			case "Data":
				var data ExtensionData
				if err := d.DecodeElement(&data, &elem); err != nil {
					return err
				}
				e.Data = &data
			case "ExtendData":
				var loc StLoc
				if err := d.DecodeElement(&loc, &elem); err != nil {
					return err
				}
				e.ExtendData = &loc
			default:
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem == start.End() {
				return nil
			}
		}
	}
}
