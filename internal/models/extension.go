package models

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"time"
)

// Extensions 扩展列表容器
type Extensions struct {
	XMLName    xml.Name    `xml:"Extensions"`
	Xmlns      string      `xml:"xmlns,attr"`
	Extensions []Extension `xml:"Extension"`
}

// Extension 单个扩展定义
type Extension struct {
	AppName    string          `xml:"AppName,attr"`
	Company    *string         `xml:"Company,attr,omitempty"`
	AppVersion *string         `xml:"AppVersion,attr,omitempty"`
	Date       *time.Time      `xml:"Date,attr,omitempty"`
	RefID      StRefID         `xml:"RefId,attr"`
	Properties []ExtensionProp `xml:"Property,omitempty"`
	Data       *interface{}    `xml:"Data,omitempty"`
	ExtendData *StLoc          `xml:"ExtendData,omitempty"`
}

// ExtensionProp 扩展属性
type ExtensionProp struct {
	Name  string  `xml:"Name,attr"`
	Type  *string `xml:"Type,attr,omitempty"`
	Value string  `xml:",chardata"`
}

// UnmarshalXML 自定义Extension解析
func (e *Extension) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
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
			if id, err := strconv.ParseUint(attr.Value, 10, 32); err == nil {
				e.RefID = StRefID(id)
			}
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
				var prop ExtensionProp
				if err := d.DecodeElement(&prop, &elem); err != nil {
					return err
				}
				e.Properties = append(e.Properties, prop)
			case "Data":
				var data interface{}
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
			}
		case xml.EndElement:
			if elem == start.End() {
				return nil
			}
		}
	}
}

// MarshalXML 自定义Extension序列化（修正版）
func (e *Extension) MarshalXML(enc *xml.Encoder, start xml.StartElement) error {
	// 创建开始元素
	start.Name.Local = "Extension"
	start.Attr = []xml.Attr{
		{Name: xml.Name{Local: "AppName"}, Value: e.AppName},
		{Name: xml.Name{Local: "RefId"}, Value: fmt.Sprintf("%d", e.RefID)},
	}

	if e.Company != nil {
		start.Attr = append(start.Attr, xml.Attr{
			Name:  xml.Name{Local: "Company"},
			Value: *e.Company,
		})
	}
	if e.AppVersion != nil {
		start.Attr = append(start.Attr, xml.Attr{
			Name:  xml.Name{Local: "AppVersion"},
			Value: *e.AppVersion,
		})
	}
	if e.Date != nil {
		start.Attr = append(start.Attr, xml.Attr{
			Name:  xml.Name{Local: "Date"},
			Value: e.Date.Format(time.RFC3339),
		})
	}

	// 开始编码Extension元素
	if err := enc.EncodeToken(start); err != nil {
		return err
	}

	// 编码Property元素（修正部分）
	for _, prop := range e.Properties {
		propStart := xml.StartElement{
			Name: xml.Name{Local: "Property"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "Name"}, Value: prop.Name},
			},
		}
		if prop.Type != nil {
			propStart.Attr = append(propStart.Attr, xml.Attr{
				Name:  xml.Name{Local: "Type"},
				Value: *prop.Type,
			})
		}

		if err := enc.EncodeToken(propStart); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.CharData(prop.Value)); err != nil {
			return err
		}
		if err := enc.EncodeToken(propStart.End()); err != nil {
			return err
		}
	}

	// 编码Data元素
	if e.Data != nil {
		dataStart := xml.StartElement{Name: xml.Name{Local: "Data"}}
		if err := enc.EncodeToken(dataStart); err != nil {
			return err
		}
		if err := enc.Encode(e.Data); err != nil {
			return err
		}
		if err := enc.EncodeToken(dataStart.End()); err != nil {
			return err
		}
	}

	// 编码ExtendData元素
	if e.ExtendData != nil {
		if err := enc.EncodeElement(*e.ExtendData, xml.StartElement{
			Name: xml.Name{Local: "ExtendData"},
		}); err != nil {
			return err
		}
	}

	// 结束Extension元素
	return enc.EncodeToken(start.End())
}
