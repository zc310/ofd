package spec

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// StPos 是 GB/T 33190—2016 §7.3 表2的点坐标，编码为以空格分隔的 X、Y。
// 解码额外保留既有逗号分隔的兼容写法。
type StPos struct {
	X float64
	Y float64
}

var (
	errNonFinite      = errors.New("数值必须是有限数值")
	errPositionFormat = errors.New("无效的位置格式，应为'x,y'形式")
)

//nolint:err113 // 迁移时保留既有解码错误文本。
func parseFiniteFloat(value, name string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("解析%s失败: %w", name, err)
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%s必须是有限数值", name)
	}
	return parsed, nil
}

// UnmarshalXML 解码点坐标元素。
func (p *StPos) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value string
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	return p.parseFromString(value)
}

// UnmarshalXMLAttr 解码点坐标属性。
func (p *StPos) UnmarshalXMLAttr(attr xml.Attr) error {
	return p.parseFromString(attr.Value)
}

func (p *StPos) parseFromString(value string) error {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) != 2 {
		return errPositionFormat
	}
	var err error
	p.X, err = parseFiniteFloat(parts[0], "X坐标")
	if err != nil {
		return err
	}
	p.Y, err = parseFiniteFloat(parts[1], "Y坐标")
	return err
}

// MarshalXML 按表2编码点坐标元素。
func (p StPos) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return marshalNumberElement(e, start, []float64{p.X, p.Y})
}

// MarshalXMLAttr 按表2编码点坐标属性。
func (p StPos) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return marshalNumberAttr(name, []float64{p.X, p.Y})
}

// StBox 是 §7.3 表2的矩形区域，字段顺序为 X、Y、Width、Height。
// 标准要求宽高大于零；解码为兼容既有文档保留零值和负值，不在这里执行文档校验。
type StBox struct {
	X      float64 `xml:"X,attr"`
	Y      float64 `xml:"Y,attr"`
	Width  float64 `xml:"Width,attr"`
	Height float64 `xml:"Height,attr"`
}

// IsFinite 判断矩形的坐标和尺寸是否都是有限数值。
func (b StBox) IsFinite() bool {
	return !math.IsNaN(b.X) && !math.IsInf(b.X, 0) &&
		!math.IsNaN(b.Y) && !math.IsInf(b.Y, 0) &&
		!math.IsNaN(b.Width) && !math.IsInf(b.Width, 0) &&
		!math.IsNaN(b.Height) && !math.IsInf(b.Height, 0)
}

//nolint:err113 // 迁移时保留既有解码错误文本。
func (b *StBox) parseFromString(value string) error {
	var parts [4]string
	if count := scanFields(value, parts[:]); count != len(parts) {
		return fmt.Errorf("格式错误: 期望4个数值(X Y Width Height)，实际得到%d个", count)
	}
	fields := []*float64{&b.X, &b.Y, &b.Width, &b.Height}
	fieldNames := []string{"x坐标", "y坐标", "width", "height"}
	for i, part := range parts {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return fmt.Errorf("%s解析失败: %w", fieldNames[i], err)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s必须是有限数值", fieldNames[i])
		}
		*fields[i] = value
	}
	return nil
}

// UnmarshalXML 解码矩形区域元素。
func (b *StBox) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value string
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	return b.parseFromString(value)
}

// UnmarshalXMLAttr 解码矩形区域属性。
func (b *StBox) UnmarshalXMLAttr(attr xml.Attr) error {
	return b.parseFromString(attr.Value)
}

// MarshalXML 按表2编码矩形区域元素。
func (b StBox) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return marshalNumberElement(e, start, []float64{b.X, b.Y, b.Width, b.Height})
}

// MarshalXMLAttr 按表2编码矩形区域属性。
func (b StBox) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return marshalNumberAttr(name, []float64{b.X, b.Y, b.Width, b.Height})
}

// String 返回按 X、Y、Width、Height 排列的字符串。
func (b *StBox) String() string {
	return fmt.Sprintf("%g %g %g %g", b.X, b.Y, b.Width, b.Height)
}

// Area 返回矩形区域的面积。
func (b *StBox) Area() float64 { return b.Width * b.Height }

// IsPortrait 判断矩形是否纵向放置。
func (b *StBox) IsPortrait() bool { return b.Height > b.Width }

// CopyAndShift 返回按给定矩形的原点偏移后的副本。
func (b *StBox) CopyAndShift(box *StBox) StBox {
	return StBox{X: b.X + box.X, Y: b.Y + box.Y, Width: b.Width, Height: b.Height}
}

// CTM 是 GB/T 33190—2016 §8.5 的六分量仿射变换矩阵。
// 分量顺序为 a、b、c、d、e、f；点变换为 (a*x+c*y+e, b*x+d*y+f)。
type CTM [6]float64

// IdentityCTM 返回独立的单位矩阵，调用方可修改而不会影响其他使用者。
func IdentityCTM() CTM { return CTM{1, 0, 0, 1, 0, 0} }

// UnmarshalXMLAttr 解码六个以空白分隔的有限数值。
//
//nolint:err113 // 迁移时保留既有解码错误文本。
func (c *CTM) UnmarshalXMLAttr(attr xml.Attr) error {
	var parts [6]string
	if count := scanFields(attr.Value, parts[:]); count != len(parts) {
		return fmt.Errorf("OFD CTM需要6个值，但得到 %d 个", count)
	}
	for i, part := range parts {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return fmt.Errorf("解析第 %d 个值 '%s' 失败: %w", i+1, part, err)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("第 %d 个值 '%s' 必须是有限数值", i+1, part)
		}
		c[i] = value
	}
	return nil
}

// UnmarshalXML 解码矩阵元素。
func (c *CTM) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value string
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	return c.UnmarshalXMLAttr(xml.Attr{Value: value})
}

// MarshalXML 将矩阵编码为六个数值，不使用 String 的诊断方括号。
func (c CTM) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return marshalNumberElement(e, start, c[:])
}

// MarshalXMLAttr 将矩阵编码为空格分隔的六个数值。
func (c CTM) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return marshalNumberAttr(name, c[:])
}

// IsFinite 判断矩阵的每个分量是否有限；nil 表示单位矩阵。
func (c *CTM) IsFinite() bool {
	if c == nil {
		return true
	}
	for _, value := range c {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

// IsInvertible 判断矩阵是否可逆；nil 表示单位矩阵。
func (c *CTM) IsInvertible() bool {
	return c == nil || c.IsFinite() && c[0]*c[3]-c[1]*c[2] != 0
}

// String 返回用于诊断的矩阵字符串，不作为 XML 编码。
func (c *CTM) String() string {
	return fmt.Sprintf("[%.4f %.4f %.4f %.4f %.4f %.4f]", c[0], c[1], c[2], c[3], c[4], c[5])
}

// TransformPoint 变换点坐标。
func (c *CTM) TransformPoint(p StPos) (float64, float64) { return c.Transform(p.X, p.Y) }

// Transform 变换给定的横纵坐标。
func (c *CTM) Transform(x, y float64) (float64, float64) {
	return c[0]*x + c[2]*y + c[4], c[1]*x + c[3]*y + c[5]
}

// YScale 返回纵轴方向的缩放量。
func (c *CTM) YScale() float64 { return math.Sqrt(c[2]*c[2] + c[3]*c[3]) }

// RotationAngle 返回旋转角度，单位为弧度。
func (c *CTM) RotationAngle() float64 { return math.Atan2(c[1], c[0]) }

// RotationAngleDegrees 返回旋转角度，单位为度。
func (c *CTM) RotationAngleDegrees() float64 { return c.RotationAngle() * 180 / math.Pi }

// Multiply 返回当前矩阵乘以 other 的结果，不修改输入矩阵。
func (c *CTM) Multiply(other *CTM) *CTM {
	return &CTM{
		c[0]*other[0] + c[2]*other[1],
		c[1]*other[0] + c[3]*other[1],
		c[0]*other[2] + c[2]*other[3],
		c[1]*other[2] + c[3]*other[3],
		c[0]*other[4] + c[2]*other[5] + c[4],
		c[1]*other[4] + c[3]*other[5] + c[5],
	}
}

func marshalFiniteNumbers(values []float64) ([]byte, error) {
	var data []byte
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("第 %d 个值: %w", i+1, errNonFinite)
		}
		if i > 0 {
			data = append(data, ' ')
		}
		data = strconv.AppendFloat(data, value, 'g', -1, 64)
	}
	return data, nil
}

func marshalNumberElement(e *xml.Encoder, start xml.StartElement, values []float64) error {
	data, err := marshalFiniteNumbers(values)
	if err != nil {
		return err
	}
	return e.EncodeElement(string(data), start)
}

func marshalNumberAttr(name xml.Name, values []float64) (xml.Attr, error) {
	data, err := marshalFiniteNumbers(values)
	if err != nil {
		return xml.Attr{}, err
	}
	return xml.Attr{Name: name, Value: string(data)}, nil
}
