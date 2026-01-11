package models

import (
	"encoding/xml"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"
)

// StID 标识符类型
type StID uint64

func (p *StID) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}

	*p = StID(val)
	return nil
}
func (p *StID) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 将无符号整型转换为字符串
	str := strconv.FormatUint(uint64(*p), 10)
	return e.EncodeElement(str, start)
}

// StRefID 引用ID类型
type StRefID StID

// StArray 数组字符串类型
type StArray []string

// UnmarshalXML 从XML字符串解析StArray
func (p *StArray) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return fmt.Errorf("XML解码失败: %v", err)
	}

	// 拆分空格分隔的字符串
	// 使用Fields而不是Split，可以自动处理多个空格的情况
	*p = strings.Fields(s)
	return nil
}

// MarshalXML 将StArray序列化为XML字符串
func (p *StArray) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 用空格拼接字符串数组
	str := strings.Join(*p, " ")
	return e.EncodeElement(str, start)
}

// StPos 位置坐标类型
type StPos struct {
	X float64
	Y float64
}

// UnmarshalXML 从 XML 字符串解析 StPos
func (p *StPos) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return fmt.Errorf("XML解码错误: %v", err)
	}

	return p.parseFromString(s)
}
func (p *StPos) UnmarshalXMLAttr(attr xml.Attr) error {
	return p.parseFromString(attr.Value)
}

// MarshalXML 将 StPos 序列化为 XML 字符串
func (p *StPos) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 格式化为 "x,y" 字符串
	str := fmt.Sprintf("%f,%f", p.X, p.Y)
	return e.EncodeElement(str, start)
}

func (p *StPos) parseFromString(s string) error {
	// 假设 XML 格式为 "x,y" 例如 "1.23,4.56"
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' '
	})
	if len(parts) != 2 {
		return fmt.Errorf("无效的位置格式，应为'x,y'形式")
	}

	var err error
	p.X, err = strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return fmt.Errorf("解析X坐标失败: %v", err)
	}

	p.Y, err = strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("解析Y坐标失败: %v", err)
	}
	return nil
}

// DestType 目标类型枚举
type DestType string

const (
	DestTypeXYZ  DestType = "XYZ"
	DestTypeFit  DestType = "Fit"
	DestTypeFitH DestType = "FitH"
	DestTypeFitV DestType = "FitV"
	DestTypeFitR DestType = "FitR"
)

// CtDest 目标定义
type CtDest struct {
	Type   DestType `xml:"Type,attr"`
	PageID StRefID  `xml:"PageID,attr"`
	Left   *float64 `xml:"Left,attr,omitempty"`
	Top    *float64 `xml:"Top,attr,omitempty"`
	Right  *float64 `xml:"Right,attr,omitempty"`
	Bottom *float64 `xml:"Bottom,attr,omitempty"`
	Zoom   *float64 `xml:"Zoom,attr,omitempty"`
}

// CtPageArea 页面区域定义
type CtPageArea struct {
	PhysicalBox    StBox  `xml:"PhysicalBox"`
	ApplicationBox *StBox `xml:"ApplicationBox,omitempty"`
	ContentBox     *StBox `xml:"ContentBox,omitempty"`
	BleedBox       *StBox `xml:"BleedBox,omitempty"`
}

// ActionEvent 动作事件类型
type ActionEvent string

const (
	ActionEventDO    ActionEvent = "DO"
	ActionEventPO    ActionEvent = "PO"
	ActionEventClick ActionEvent = "CLICK"
)

// CtAction 动作定义
type CtAction struct {
	Event  ActionEvent `xml:"Event,attr"`
	Region *CtRegion   `xml:"Region,omitempty"`

	// 动作选择项
	Goto  *ActionGoto  `xml:"Goto,omitempty"`
	URI   *ActionURI   `xml:"URI,omitempty"`
	GotoA *ActionGotoA `xml:"GotoA,omitempty"`
	Sound *ActionSound `xml:"Sound,omitempty"`
	Movie *ActionMovie `xml:"Movie,omitempty"`
}

// ActionGoto 跳转动作
type ActionGoto struct {
	Dest     *CtDest `xml:"Dest,omitempty"`
	Bookmark *struct {
		Name string `xml:"Name,attr"`
	} `xml:"Bookmark,omitempty"`
}

// ActionURI URI动作
type ActionURI struct {
	URI    string  `xml:"URI,attr"`
	Base   *string `xml:"Base,attr,omitempty"`
	Target *string `xml:"Target,attr,omitempty"`
}

// ActionGotoA 附件跳转动作
type ActionGotoA struct {
	AttachID  string `xml:"AttachID,attr"`
	NewWindow bool   `xml:"NewWindow,attr,omitempty"`
}

// ActionSound 声音动作
type ActionSound struct {
	ResourceID  StRefID `xml:"ResourceID,attr"`
	Volume      *int    `xml:"Volume,attr,omitempty"`
	Repeat      *bool   `xml:"Repeat,attr,omitempty"`
	Synchronous *bool   `xml:"Synchronous,attr,omitempty"`
}

// MovieOperator 影片操作类型
type MovieOperator string

const (
	MovieOperatorPlay   MovieOperator = "Play"
	MovieOperatorStop   MovieOperator = "Stop"
	MovieOperatorPause  MovieOperator = "Pause"
	MovieOperatorResume MovieOperator = "Resume"
)

// ActionMovie 影片动作
type ActionMovie struct {
	ResourceID StRefID       `xml:"ResourceID,attr"`
	Operator   MovieOperator `xml:"Operator,attr,omitempty"`
}

// CtRegion 区域定义
type CtRegion struct {
	Areas []Area `xml:"Area"`
}

// Area 区域中的单个区域
type Area struct {
	Start StPos  `xml:"Start,attr"`
	Paths []Path `xml:",any"`
}

// Path 路径元素
type Path struct {
	XMLName xml.Name
	// 公共属性
	Point1 *StPos `xml:"Point1,attr,omitempty"`
	Point2 *StPos `xml:"Point2,attr,omitempty"`
	Point3 *StPos `xml:"Point3,attr,omitempty"`

	// Arc特有属性
	SweepDirection *bool    `xml:"SweepDirection,attr,omitempty"`
	LargeArc       *bool    `xml:"LargeArc,attr,omitempty"`
	RotationAngle  *float64 `xml:"RotationAngle,attr,omitempty"`
	EllipseSize    *StArray `xml:"EllipseSize,attr,omitempty"`
	EndPoint       *StPos   `xml:"EndPoint,attr,omitempty"`
}

// CustomDatas 自定义数据集合
type CustomDatas struct {
	CustomData []CustomData `xml:"CustomData"`
}

// CustomData 自定义数据
type CustomData struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

// DateTime 自定义时间类型，用于解析OFD中的时间格式
type DateTime struct {
	time.Time
}

// UnmarshalXML 自定义XML元素解析方法
func (t *DateTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	err := d.DecodeElement(&v, &start)
	if err != nil {
		return err
	}
	return t.parseTime(v)
}

// UnmarshalXMLAttr 自定义XML属性解析方法
func (t *DateTime) UnmarshalXMLAttr(attr xml.Attr) error {
	return t.parseTime(attr.Value)
}

// parseTime 解析时间的通用方法
func (t *DateTime) parseTime(v string) error {
	// 尝试解析多种可能的时间格式
	formats := []string{
		"2006-01-02",
		"2006-1-2",
		"20060102",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
		"01/02/2006 3:04:05 PM",
	}

	for _, format := range formats {
		if parsedTime, err := time.Parse(format, v); err == nil {
			*t = DateTime{parsedTime}
			return nil
		}
	}

	return fmt.Errorf("无法解析时间: %s", v)
}

// MarshalXML 自定义XML序列化方法
func (t *DateTime) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if t.IsZero() {
		return nil
	}

	// 判断时分秒是否为0
	if t.Time.Hour() == 0 && t.Time.Minute() == 0 && t.Time.Second() == 0 {
		return e.EncodeElement(t.Format("2006-01-02"), start)
	}

	// 否则使用完整时间格式
	return e.EncodeElement(t.Format("2006-01-02T15:04:05"), start)
}

// MarshalXMLAttr 自定义XML属性序列化方法
func (t *DateTime) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	if t.IsZero() {
		return xml.Attr{}, nil
	}

	var value string
	// 判断时分秒是否为0
	if t.Time.Hour() == 0 && t.Time.Minute() == 0 && t.Time.Second() == 0 {
		value = t.Format("2006-01-02")
	} else {
		value = t.Format("2006-01-02T15:04:05")
	}

	return xml.Attr{
		Name:  name,
		Value: value,
	}, nil
}

type Color struct {
	color.RGBA
}

// UnmarshalXML 解析 XML 元素
func (c *Color) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}
	return c.parse(s)
}

// UnmarshalXMLAttr 解析 XML 属性
func (c *Color) UnmarshalXMLAttr(attr xml.Attr) error {
	return c.parse(attr.Value)
}

// MarshalXML 生成 XML
func (c Color) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(c.String(), start)
}

// MarshalXMLAttr 生成 XML 属性
func (c Color) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return xml.Attr{
		Name:  name,
		Value: c.String(),
	}, nil
}

// 解析字符串 "156 82 35" 或 "156 82 35 255"
func (c *Color) parse(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		*c = Color{
			RGBA: color.RGBA{R: 0, G: 0, B: 0, A: 255},
		}
		return nil
	}

	parts := strings.Fields(s)
	if len(parts) != 3 && len(parts) != 4 {
		return fmt.Errorf("invalid color format: %s, expected 'R G B' or 'R G B A'", s)
	}

	// 解析 RGB
	values := [4]uint8{0, 0, 0, 255}
	for i := 0; i < len(parts); i++ {
		val, err := strconv.Atoi(parts[i])
		if err != nil {
			return fmt.Errorf("invalid number '%s' in color: %v", parts[i], err)
		}
		if val < 0 || val > 255 {
			return fmt.Errorf("color value out of range 0-255: %d", val)
		}
		values[i] = uint8(val)
	}

	// 如果没有 Alpha，使用 255
	if len(parts) == 3 {
		values[3] = 255
	}

	c.RGBA = color.RGBA{
		R: values[0],
		G: values[1],
		B: values[2],
		A: values[3],
	}
	return nil
}

// String 返回 "R G B" 或 "R G B A" 格式
func (c Color) String() string {
	if c.A == 255 {
		return fmt.Sprintf("%d %d %d", c.R, c.G, c.B)
	}
	return fmt.Sprintf("%d %d %d %d", c.R, c.G, c.B, c.A)
}

var IdentityMatrix = CTM{1, 0, 0, 1, 0, 0}

// CTM 表示OFD图元变换矩阵
type CTM [6]float64

// UnmarshalXMLAttr 实现XML属性解析
func (c *CTM) UnmarshalXMLAttr(attr xml.Attr) error {
	parts := strings.Fields(strings.TrimSpace(attr.Value))
	if len(parts) != 6 {
		return fmt.Errorf("OFD CTM需要6个值，但得到 %d 个", len(parts))
	}

	for i, part := range parts {
		val, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return fmt.Errorf("解析第 %d 个值 '%s' 失败: %v", i+1, part, err)
		}
		c[i] = val
	}

	return nil
}

// String 返回字符串表示
func (c *CTM) String() string {
	return fmt.Sprintf("[%.4f %.4f %.4f %.4f %.4f %.4f]",
		c[0], c[1], c[2], c[3], c[4], c[5])
}

// TransformPoint 变换点坐标
func (c *CTM) TransformPoint(p StPos) (float64, float64) {
	return c.Transform(p.X, p.Y)
}
func (c *CTM) Transform(x, y float64) (float64, float64) {
	return c[0]*x + c[2]*y + c[4], c[1]*x + c[3]*y + c[5]
}
func (c *CTM) YScale() float64 {
	return math.Sqrt(c[2]*c[2] + c[3]*c[3])
}

// RotationAngle 提取旋转角度（弧度）
func (c *CTM) RotationAngle() float64 {
	// 从矩阵中提取旋转部分
	// 对于纯旋转矩阵：a = cosθ, b = sinθ, c = -sinθ, d = cosθ
	// 所以旋转角度 θ = atan2(b, a)
	return math.Atan2(c[1], c[0])
}

// RotationAngleDegrees 提取旋转角度（度）
func (c *CTM) RotationAngleDegrees() float64 {
	radians := c.RotationAngle()
	return radians * 180.0 / math.Pi
}

// Multiply 矩阵乘法：计算 this × other
// CTM 是3x2矩阵，但实际上是3x3齐次坐标的2D仿射变换矩阵
// 矩阵形式：
// [ a  c  e ]
// [ b  d  f ]
// [ 0  0  1 ]
func (c *CTM) Multiply(other *CTM) *CTM {
	// 矩阵乘法公式：
	// result[0] = c[0]*other[0] + c[2]*other[1]  // a = a1*a2 + c1*b2
	// result[1] = c[1]*other[0] + c[3]*other[1]  // b = b1*a2 + d1*b2
	// result[2] = c[0]*other[2] + c[2]*other[3]  // c = a1*c2 + c1*d2
	// result[3] = c[1]*other[2] + c[3]*other[3]  // d = b1*c2 + d1*d2
	// result[4] = c[0]*other[4] + c[2]*other[5] + c[4]  // e = a1*e2 + c1*f2 + e1
	// result[5] = c[1]*other[4] + c[3]*other[5] + c[5]  // f = b1*e2 + d1*f2 + f1

	return &CTM{
		c[0]*other[0] + c[2]*other[1],        // a
		c[1]*other[0] + c[3]*other[1],        // b
		c[0]*other[2] + c[2]*other[3],        // c
		c[1]*other[2] + c[3]*other[3],        // d
		c[0]*other[4] + c[2]*other[5] + c[4], // e
		c[1]*other[4] + c[3]*other[5] + c[5], // f
	}
}
