package models

import (
	"encoding/xml"
	"fmt"
	"strconv"
)

// 手写解码页面对象元素，避免 encoding/xml 对每个字段做反射匹配与类型信息查找。
// 行为与原先由 xml 标签驱动的一致：未知属性与未知子元素被忽略，属性/子元素值解析
// 失败返回错误，默认值由零值表达。

func decodeFloatAttr(value, name string) (float64, error) {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("解析%s失败: %w", name, err)
	}
	return v, nil
}

func decodeIntAttr(value, name string) (int, error) {
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析%s失败: %w", name, err)
	}
	return int(v), nil
}

func decodeBoolAttr(value, name string) (bool, error) {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("解析%s失败: %w", name, err)
	}
	return v, nil
}

func decodeAlphaAttr(value string) (*uint8, error) {
	n, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return nil, fmt.Errorf("解析Alpha失败: %w", err)
	}
	v := uint8(n)
	return &v, nil
}

// decodeGraphicAttrs 解析图元通用属性，行为与原 xml:"*,attr" 字段一致。
func (g *CTGraphicUnit) decodeGraphicAttrs(start xml.StartElement) error {
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "Boundary":
			if err := g.Boundary.UnmarshalXMLAttr(attr); err != nil {
				return err
			}
		case "Name":
			g.Name = attr.Value
		case "Visible":
			_ = g.Visible.UnmarshalXMLAttr(attr)
		case "CTM":
			if g.CTM == nil {
				g.CTM = new(CTM)
			}
			if err := g.CTM.UnmarshalXMLAttr(attr); err != nil {
				return err
			}
		case "DrawParam":
			_ = g.DrawParam.UnmarshalXMLAttr(attr)
		case "LineWidth":
			v, err := decodeFloatAttr(attr.Value, "LineWidth")
			if err != nil {
				return err
			}
			g.LineWidth = v
		case "Cap":
			g.Cap = attr.Value
		case "Join":
			g.Join = attr.Value
		case "MiterLimit":
			v, err := decodeFloatAttr(attr.Value, "MiterLimit")
			if err != nil {
				return err
			}
			g.MiterLimit = v
		case "DashOffset":
			v, err := decodeFloatAttr(attr.Value, "DashOffset")
			if err != nil {
				return err
			}
			g.DashOffset = v
		case "DashPattern":
			if g.DashPattern == nil {
				g.DashPattern = new(StArrayF)
			}
			if err := g.DashPattern.UnmarshalXMLAttr(attr); err != nil {
				return err
			}
		case "Alpha":
			v, err := decodeAlphaAttr(attr.Value)
			if err != nil {
				return err
			}
			g.Alpha = v
		}
	}
	return nil
}

// decodeGraphicUnitChildElement 解析以子元素形式编码的图元通用属性（历史文档兼容），
// 写入 Element 后备字段，对象解析收尾时由 normalizeDrawParams 提升。
func decodeGraphicUnitChildElement(d *xml.Decoder, elem *xml.StartElement, g *CTGraphicUnit) error {
	var text string
	if err := d.DecodeElement(&text, elem); err != nil {
		return fmt.Errorf("XML解码失败: %w", err)
	}
	switch elem.Name.Local {
	case "DrawParam":
		var id StID
		_ = id.UnmarshalText([]byte(text))
		g.DrawParamElement = &id
	case "LineWidth":
		v, err := decodeFloatAttr(text, "LineWidth")
		if err != nil {
			return err
		}
		g.LineWidthElement = &v
	case "Cap":
		g.CapElement = &text
	case "Join":
		g.JoinElement = &text
	case "MiterLimit":
		v, err := decodeFloatAttr(text, "MiterLimit")
		if err != nil {
			return err
		}
		g.MiterLimitElement = &v
	case "DashOffset":
		v, err := decodeFloatAttr(text, "DashOffset")
		if err != nil {
			return err
		}
		g.DashOffsetElement = &v
	case "DashPattern":
		var a StArrayF
		if err := a.UnmarshalXMLAttr(xml.Attr{Value: text}); err != nil {
			return err
		}
		g.DashPatternElement = &a
	}
	return nil
}

// decodeObjectSkip 忽略对象中不认识的子元素。
func decodeObjectSkip(d *xml.Decoder) error {
	return d.Skip()
}

// UnmarshalXML 手写解码文字对象。
func (o *TextObject) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := o.CTGraphicUnit.decodeGraphicAttrs(start); err != nil {
		return err
	}
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "ID":
			_ = o.ID.UnmarshalText([]byte(attr.Value))
		case "Font":
			_ = o.Font.UnmarshalXMLAttr(attr)
		case "Size":
			v, err := decodeFloatAttr(attr.Value, "Size")
			if err != nil {
				return err
			}
			o.Size = v
		case "Stroke":
			v, err := decodeBoolAttr(attr.Value, "Stroke")
			if err != nil {
				return err
			}
			o.Stroke = v
		case "Fill":
			_ = o.Fill.UnmarshalXMLAttr(attr)
		case "HScale":
			v, err := decodeFloatAttr(attr.Value, "HScale")
			if err != nil {
				return err
			}
			o.HScale = v
		case "ReadDirection":
			v, err := decodeIntAttr(attr.Value, "ReadDirection")
			if err != nil {
				return err
			}
			o.ReadDirection = v
		case "CharDirection":
			v, err := decodeIntAttr(attr.Value, "CharDirection")
			if err != nil {
				return err
			}
			o.CharDirection = v
		case "Weight":
			v, err := decodeIntAttr(attr.Value, "Weight")
			if err != nil {
				return err
			}
			o.Weight = v
		case "Italic":
			v, err := decodeBoolAttr(attr.Value, "Italic")
			if err != nil {
				return err
			}
			o.Italic = v
		}
	}

	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch elem := token.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "FillColor":
				var c CTColor
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.FillColor = &c
			case "StrokeColor":
				var c CTColor
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.StrokeColor = &c
			case "TextCode":
				var tc TextCode
				if err := d.DecodeElement(&tc, &elem); err != nil {
					return err
				}
				o.TextCode = append(o.TextCode, tc)
			case "CGTransform":
				var g CTCGTransform
				if err := d.DecodeElement(&g, &elem); err != nil {
					return err
				}
				o.CGTransform = append(o.CGTransform, g)
			case "Actions":
				var a Actions
				if err := d.DecodeElement(&a, &elem); err != nil {
					return err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := decodeGraphicUnitChildElement(d, &elem, &o.CTGraphicUnit); err != nil {
					return err
				}
			default:
				if err := decodeObjectSkip(d); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem == start.End() {
				o.CTGraphicUnit.normalizeDrawParams()
				return nil
			}
		}
	}
}

// UnmarshalXML 手写解码路径对象。
func (o *PathObject) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := o.CTGraphicUnit.decodeGraphicAttrs(start); err != nil {
		return err
	}
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "ID":
			_ = o.ID.UnmarshalText([]byte(attr.Value))
		case "Stroke":
			_ = o.Stroke.UnmarshalXMLAttr(attr)
		case "Fill":
			v, err := decodeBoolAttr(attr.Value, "Fill")
			if err != nil {
				return err
			}
			o.Fill = v
		case "Rule":
			o.Rule = attr.Value
		}
	}

	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch elem := token.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "StrokeColor":
				var c CTColor
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.StrokeColor = &c
			case "FillColor":
				var c CTColor
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.FillColor = &c
			case "AbbreviatedData":
				if err := o.AbbreviatedData.UnmarshalXML(d, elem); err != nil {
					return err
				}
			case "Actions":
				var a Actions
				if err := d.DecodeElement(&a, &elem); err != nil {
					return err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := decodeGraphicUnitChildElement(d, &elem, &o.CTGraphicUnit); err != nil {
					return err
				}
			default:
				if err := decodeObjectSkip(d); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem == start.End() {
				o.CTGraphicUnit.normalizeDrawParams()
				return nil
			}
		}
	}
}

// UnmarshalXML 手写解码图像对象。
func (o *ImageObject) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := o.CTGraphicUnit.decodeGraphicAttrs(start); err != nil {
		return err
	}
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "ID":
			_ = o.ID.UnmarshalText([]byte(attr.Value))
		case "ResourceID":
			_ = o.ResourceID.UnmarshalXMLAttr(attr)
		case "Substitution":
			_ = o.Substitution.UnmarshalXMLAttr(attr)
		case "ImageMask":
			_ = o.ImageMask.UnmarshalXMLAttr(attr)
		}
	}

	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch elem := token.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "Border":
				var b Border
				if err := d.DecodeElement(&b, &elem); err != nil {
					return err
				}
				o.Border = &b
			case "Actions":
				var a Actions
				if err := d.DecodeElement(&a, &elem); err != nil {
					return err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := decodeGraphicUnitChildElement(d, &elem, &o.CTGraphicUnit); err != nil {
					return err
				}
			default:
				if err := decodeObjectSkip(d); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem == start.End() {
				o.CTGraphicUnit.normalizeDrawParams()
				return nil
			}
		}
	}
}

// UnmarshalXML 手写解码复合对象。
func (o *CompositeObject) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if err := o.CTGraphicUnit.decodeGraphicAttrs(start); err != nil {
		return err
	}
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "ID":
			_ = o.ID.UnmarshalText([]byte(attr.Value))
		case "ResourceID":
			_ = o.ResourceID.UnmarshalXMLAttr(attr)
		}
	}

	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch elem := token.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "Actions":
				var a Actions
				if err := d.DecodeElement(&a, &elem); err != nil {
					return err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := d.DecodeElement(&c, &elem); err != nil {
					return err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := decodeGraphicUnitChildElement(d, &elem, &o.CTGraphicUnit); err != nil {
					return err
				}
			default:
				if err := decodeObjectSkip(d); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem == start.End() {
				o.CTGraphicUnit.normalizeDrawParams()
				return nil
			}
		}
	}
}
