package models

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// PageAnnot 页面注解容器
type PageAnnot struct {
	XMLName xml.Name `xml:"PageAnnot"`
	Xmlns   string   `xml:"xmlns,attr"`
	Annots  []*Annot `xml:"Annot"`
}

// Annot 单个注解定义
type Annot struct {
	ID          string      `xml:"ID,attr"`
	Type        AnnotType   `xml:"Type,attr"`
	Creator     string      `xml:"Creator,attr"`
	LastModDate DateTime    `xml:"LastModDate,attr"`
	Visible     bool        `xml:"Visible,attr,omitempty"`
	Subtype     string      `xml:"Subtype,attr,omitempty"`
	Print       bool        `xml:"Print,attr,omitempty"`
	NoZoom      bool        `xml:"NoZoom,attr,omitempty"`
	NoRotate    bool        `xml:"NoRotate,attr,omitempty"`
	ReadOnly    bool        `xml:"ReadOnly,attr,omitempty"`
	Remark      *string     `xml:"Remark,omitempty"`
	Parameters  *Params     `xml:"Parameters,omitempty"`
	Appearance  *Appearance `xml:"Appearance"`
}

// AnnotType 注解类型枚举
type AnnotType string

const (
	AnnotTypeLink      AnnotType = "Link"
	AnnotTypePath      AnnotType = "Path"
	AnnotTypeHighlight AnnotType = "Highlight"
	AnnotTypeStamp     AnnotType = "Stamp"
	AnnotTypeWatermark AnnotType = "Watermark"
)

// Params 参数集合
type Params struct {
	Parameters []Parameter `xml:"Parameter"`
}

// Parameter 单个参数
type Parameter struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

// Appearance 注解外观
type Appearance struct {
	Boundary *StBox `xml:"Boundary,attr,omitempty"`
	CTPageBlock
}

// StBox 盒子区域定义 (X Y Width Height)
type StBox struct {
	X      float64 `xml:"X,attr"`
	Y      float64 `xml:"Y,attr"`
	Width  float64 `xml:"Width,attr"`
	Height float64 `xml:"Height,attr"`
}

// parseFromString 从字符串解析盒子数据（私有方法）
func (b *StBox) parseFromString(value string) error {
	// 移除首尾空格并按空格分割
	parts := strings.Fields(value)
	if len(parts) != 4 {
		return fmt.Errorf("格式错误: 期望4个数值(X Y Width Height)，实际得到%d个", len(parts))
	}

	// 批量解析数值
	fields := []*float64{&b.X, &b.Y, &b.Width, &b.Height}
	fieldNames := []string{"x坐标", "y坐标", "width", "height"}

	for i, part := range parts {
		val, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return fmt.Errorf("%s解析失败: %w", fieldNames[i], err)
		}
		*fields[i] = val
	}

	// 验证数值有效性
	if b.Width < 0 {
		return fmt.Errorf("width不能为负数: %.2f", b.Width)
	}
	if b.Height < 0 {
		return fmt.Errorf("height不能为负数: %.2f", b.Height)
	}

	return nil
}

// UnmarshalXML 自定义XML元素解析方法
func (b *StBox) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	if err := d.DecodeElement(&v, &start); err != nil {
		return err
	}
	return b.parseFromString(v)
}

// UnmarshalXMLAttr 自定义XML属性解析
func (b *StBox) UnmarshalXMLAttr(attr xml.Attr) error {
	return b.parseFromString(attr.Value)
}

// MarshalXML 自定义XML序列化方法
func (p *StBox) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 格式化为"X Y Width Height"字符串
	value := fmt.Sprintf("%g %g %g %g", p.X, p.Y, p.Width, p.Height)
	return e.EncodeElement(value, start)
}

// String 返回字符串表示
func (p *StBox) String() string {
	return fmt.Sprintf("%g %g %g %g", p.X, p.Y, p.Width, p.Height)
}

// Area 计算页面面积
func (p *StBox) Area() float64 {
	return p.Width * p.Height
}

// IsPortrait 判断是否是纵向页面
func (p *StBox) IsPortrait() bool {
	return p.Height > p.Width
}

func (p *StBox) CopyAndShift(box *StBox) StBox {
	return StBox{
		X:      p.X + box.X,
		Y:      p.Y + box.Y,
		Width:  p.Width,
		Height: p.Height,
	}
}

// UnmarshalXMLAttr  自定义AnnotType解析
func (a *AnnotType) UnmarshalXMLAttr(attr xml.Attr) error {
	switch attr.Value {
	case "Link", "Path", "Highlight", "Stamp", "Watermark":
		*a = AnnotType(attr.Value)
		return nil
	default:
		return fmt.Errorf("无效的注解类型: %s", attr.Value)
	}
}
