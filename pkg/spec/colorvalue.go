package spec

import (
	"encoding/xml"
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// ColorValue 保存 GB/T 33190—2016 §8.3.2 表26/27的原始颜色通道值。
// RGBA 仅是既有的无颜色空间8位回退表示，不是按颜色空间转换后的颜色；
// 四通道在 CMYK 中不是 RGBA。实际解释须结合 ColorSpace 及 BitsPerComponent。
// XML 解码保留原始通道文本，编码时优先使用它，不使用 RGBA 回退结果。
type ColorValue struct {
	color.RGBA

	// raw 与 nraw 保存按 ST_Array 原文解析出的通道整数取值与通道数。
	//
	// 必须保留原文：通道的取值范围由颜色空间的 BitsPerComponent 决定（GB/T 33190
	// 表 27：BPC 有效时取值区间为 [0, 2^BPC-1]），同一串数字在 BPC=8 与 BPC=4 下
	// 含义完全不同——"15 0 0 0" 分别是 6% 青与满青。压成 uint8 之后两者都是 15，
	// 渲染端拿到颜色空间也无从还原。XML 属性反序列化发生在资源解析之前，
	// ColorValue.parse 拿不到 BitsPerComponent，只能把原文留给渲染端归一化。
	raw     [4]int
	nraw    int
	text    string
	textSet bool
}

// Components 返回按 ST_Array 原文解析出的通道取值与通道数。ok 为 false 表示没有
// 原始通道分量（例如空文本或直接用 RGBA 构造），此时使用方可回退到 RGBA 的8位分量。
// 返回的数组最多保存前四个分量，count 仍反映原始总数；多余分量的文本可继续 XML 往返。
func (c ColorValue) Components() ([4]int, int, bool) {
	if c.nraw == 0 {
		return [4]int{}, 0, false
	}
	return c.raw, c.nraw, true
}

// UnmarshalXML 解析 XML 元素
func (c *ColorValue) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}
	return c.parse(s)
}

// UnmarshalXMLAttr 解析 XML 属性
func (c *ColorValue) UnmarshalXMLAttr(attr xml.Attr) error {
	return c.parse(attr.Value)
}

func (c *ColorValue) parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)

	// 处理 # 开头的
	if strings.HasPrefix(s, "#") {
		val, err := strconv.ParseInt(s[1:], 16, 0)
		return int(val), err
	}

	// 其他情况让 ParseInt 自动检测
	val, err := strconv.ParseInt(s, 0, 0)
	return int(val), err
}

// 解析字符串 "156 82 35"、"156 82 35 255" 或 16 位分量 "0 0 0 65535"
//
// 分量个数不做校验：个数由颜色空间决定（GRAY 1 个、RGB 3 个、CMYK 4 个），而
// ColorValue 解析属性时拿不到颜色空间。个数与颜色空间不匹配属未定义行为，实测有文档
// 会写出 4 分量 RGB、2 分量 GRAY、5 分量 CMYK 这类值；此处若报错，异常会一路
// 冒泡到整页解析失败，一个坏颜色就能让整份文档无法转换。改为照原样保留分量个数，
// 由渲染端拿到颜色空间后决定「缺的补 0、多余的忽略」。
func (c *ColorValue) parse(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		*c = ColorValue{
			RGBA:    color.RGBA{R: 0, G: 0, B: 0, A: 255},
			text:    "",
			textSet: true,
		}
		return nil
	}

	parsed := [4]int{}
	stored := 0
	count := 0
	for part := range strings.FieldsSeq(s) {
		val, err := c.parseInt(part)
		if err != nil {
			// 只有无法解释成整数才算解析失败。越界值与负值都不是错误：GB/T 33190
			// 表 27 规定「当颜色通道的取值超出了相应的区间，则按照默认颜色来
			// 处理」，实测文档里确实存在 -1 与 65536 这类值。报错会让异常冒泡到
			// 整页解析失败，一个坏颜色就能让整份文档无法转换。
			return fmt.Errorf("颜色中的数字 '%s' 无效: %w", part, err)
		}
		// 只保留前 4 个分量：任何颜色空间最多用 4 个，多余的按未定义行为忽略。
		// 循环必须按 parsed 的长度截断，否则 Value="255 0 0 0 0" 这类 5 分量
		// 输入会越界写 panic。
		if count < len(parsed) {
			parsed[count] = val
			stored++
		}
		count++
	}

	values := [4]uint8{0, 0, 0, 255}
	// 这是没有颜色空间时的回退表示。拿不到 BitsPerComponent，只能按各通道自身的
	// 量级归一化：<=255 原样保留，<=65535 视为 16 位并右移 8 位，再大则任何合法
	// BPC 都装不下（表 25 的 BPC 上限是 16），按表 27 取默认颜色 0。
	//
	// 不再用「出现大于 255 的分量就整组按 16 位解释」的组判据：它会让
	// "65536 0 0 65535" 的第 4 个分量落到 8 位区间外而取 0，alpha 变全透明，
	// 填充整块消失——这比颜色略有偏差严重得多。真正按位深归一化的是渲染端的
	// colorSpaceComponents8。
	for i := 0; i < stored; i++ {
		switch value := parsed[i]; {
		case value < 0:
			values[i] = 0
		case value <= 255:
			values[i] = uint8(value)
		case value <= 0xFFFF:
			values[i] = uint8(value >> 8)
		default:
			values[i] = 0
		}
	}

	// 既有回退表示将第4个已保存分量映射到 A，少于4个时取完全不透明。
	// 这不表示 CMYK 的第4通道是透明度；实际解释须结合颜色空间。
	if stored != 4 {
		values[3] = 255
	}

	c.RGBA = color.RGBA{
		R: values[0],
		G: values[1],
		B: values[2],
		A: values[3],
	}
	// 保留原文，供渲染端按颜色空间的 BitsPerComponent 归一化。
	c.raw = parsed
	c.nraw = count
	c.text = s
	c.textSet = true
	return nil
}

// String 返回8位回退表示的 "R G B" 或 "R G B A"，不是原始通道的 XML 编码。
func (c ColorValue) String() string {
	if c.A == 255 {
		return fmt.Sprintf("%d %d %d", c.R, c.G, c.B)
	}
	return fmt.Sprintf("%d %d %d %d", c.R, c.G, c.B, c.A)
}

// MarshalXML 编码原始通道文本；直接用 RGBA 构造的值则沿用 String 的8位表示。
func (c ColorValue) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(c.xmlValue(), start)
}

// MarshalXMLAttr 编码原始通道属性，不将16位或 CMYK 分量缩减为8位 RGBA。
func (c ColorValue) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return xml.Attr{Name: name, Value: c.xmlValue()}, nil
}

func (c ColorValue) xmlValue() string {
	if c.textSet {
		return c.text
	}
	return c.String()
}
