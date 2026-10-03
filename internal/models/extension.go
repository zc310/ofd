package models

import (
	"encoding/xml"
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
