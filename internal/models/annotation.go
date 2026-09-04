package models

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// PageAnnot 页面注解文件的根容器。
type PageAnnot struct {
	// XMLName 页面注解根元素名称。
	XMLName xml.Name `xml:"PageAnnot"`
	// Xmlns 页面注解命名空间地址。
	Xmlns string `xml:"xmlns,attr"`
	// Annots 页面中的注解列表。
	Annots []*Annot `xml:"Annot"`
}

// Annot 单个页面注解定义。
type Annot struct {
	// ID 注解标识，在页面注解文件内唯一。
	ID string `xml:"ID,attr"`
	// Type 注解类型。
	Type AnnotType `xml:"Type,attr"`
	// Creator 创建该注解的软件或用户名称。
	Creator string `xml:"Creator,attr"`
	// LastModDate 注解最后修改时间。
	LastModDate DateTime `xml:"LastModDate,attr"`
	// Visible 表示该注释对象是否显示，未指定时默认为 true。
	Visible OptionalBool `xml:"Visible,attr,omitempty"`
	// Subtype 注解子类型。
	Subtype string `xml:"Subtype,attr,omitempty"`
	// Print 是否在打印文档时显示该注解。
	Print bool `xml:"Print,attr,omitempty"`
	// NoZoom 是否在缩放页面时保持注解大小不变。
	NoZoom bool `xml:"NoZoom,attr,omitempty"`
	// NoRotate 是否在旋转页面时保持注解方向不变。
	NoRotate bool `xml:"NoRotate,attr,omitempty"`
	// ReadOnly 是否禁止修改该注解。
	ReadOnly bool `xml:"ReadOnly,attr,omitempty"`
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
	// Boundary 注解外观的边界框。
	Boundary *StBox `xml:"Boundary,attr,omitempty"`
	// CTPageBlock 注解外观中的页面对象。
	CTPageBlock
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

// StBox 盒子区域定义，字段顺序为 X、Y、Width、Height。
type StBox struct {
	// X 盒子左上角的横坐标。
	X float64 `xml:"X,attr"`
	// Y 盒子左上角的纵坐标。
	Y float64 `xml:"Y,attr"`
	// Width 盒子宽度。
	Width float64 `xml:"Width,attr"`
	// Height 盒子高度。
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

// UnmarshalXML 从 XML 元素文本中解析盒子区域。
func (b *StBox) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	if err := d.DecodeElement(&v, &start); err != nil {
		return err
	}
	return b.parseFromString(v)
}

// UnmarshalXMLAttr 从 XML 属性文本中解析盒子区域。
func (b *StBox) UnmarshalXMLAttr(attr xml.Attr) error {
	return b.parseFromString(attr.Value)
}

// MarshalXML 将盒子区域编码为 XML 元素文本。
func (p *StBox) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 格式化为"X Y Width Height"字符串
	value := fmt.Sprintf("%g %g %g %g", p.X, p.Y, p.Width, p.Height)
	return e.EncodeElement(value, start)
}

// String 返回按 X、Y、Width、Height 顺序排列的字符串表示。
func (p *StBox) String() string {
	return fmt.Sprintf("%g %g %g %g", p.X, p.Y, p.Width, p.Height)
}

// Area 返回盒子区域的面积。
func (p *StBox) Area() float64 {
	return p.Width * p.Height
}

// IsPortrait 判断盒子区域是否为纵向，即高度大于宽度。
func (p *StBox) IsPortrait() bool {
	return p.Height > p.Width
}

// CopyAndShift 复制盒子区域，并根据给定盒子的坐标偏移复制结果。
func (p *StBox) CopyAndShift(box *StBox) StBox {
	return StBox{
		X:      p.X + box.X,
		Y:      p.Y + box.Y,
		Width:  p.Width,
		Height: p.Height,
	}
}

// UnmarshalXMLAttr 从 XML 属性中解析并校验注解类型。
func (a *AnnotType) UnmarshalXMLAttr(attr xml.Attr) error {
	switch attr.Value {
	case "Link", "Path", "Highlight", "Stamp", "Watermark":
		*a = AnnotType(attr.Value)
		return nil
	default:
		return fmt.Errorf("无效的注解类型: %s", attr.Value)
	}
}
