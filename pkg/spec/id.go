package spec

import (
	"encoding/xml"
	"hash/crc64"
	"log/slog"
	"strconv"
)

// StID 是 GB/T 33190—2016 §7.3 表2的无符号对象标识，0 表示无效标识。
// 解码保留既有非规范文档兼容行为：非十进制或溢出的值映射为 CRC64。
// 该映射不是标准要求，也不保证无碰撞；编码只输出映射后的十进制整数。
type StID uint64

// UnmarshalXML 解码对象标识元素。
func (p *StID) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value string
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	return p.UnmarshalText([]byte(value))
}

// UnmarshalXMLAttr 解码对象标识属性。
func (p *StID) UnmarshalXMLAttr(attr xml.Attr) error {
	return p.UnmarshalText([]byte(attr.Value))
}

// UnmarshalText 解码十进制对象标识，并对非规范值沿用确定性的兼容映射。
func (p *StID) UnmarshalText(text []byte) error {
	value, err := strconv.ParseUint(string(text), 10, 64)
	if err != nil {
		value = crc64.Checksum(text, crc64.MakeTable(crc64.ISO))
		slog.Error("Error parsing StID value", slog.String("value", string(text)), slog.Uint64("val", value))
	}
	*p = StID(value)
	return nil
}

// StRefID 是表2中对文档内对象标识的引用，解码与 StID 使用相同的兼容映射。
type StRefID StID

// UnmarshalXML 解码标识引用元素。
func (p *StRefID) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value string
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	return p.UnmarshalXMLAttr(xml.Attr{Value: value})
}

// UnmarshalXMLAttr 解码标识引用属性。
func (p *StRefID) UnmarshalXMLAttr(attr xml.Attr) error {
	id := StID(*p)
	if err := id.UnmarshalXMLAttr(attr); err != nil {
		return err
	}
	*p = StRefID(id)
	return nil
}
