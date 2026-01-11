package models

import (
	"encoding/xml"
	"strconv"
	"strings"
)

type StArrayF []float64

// UnmarshalXML 实现 xml.Unmarshaler 接口
func (s *StArrayF) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var content string
	if err := d.DecodeElement(&content, &start); err != nil {
		return err
	}
	return s.parseString(content)
}

// UnmarshalXMLAttr 实现 xml.UnmarshalerAttr 接口
func (s *StArrayF) UnmarshalXMLAttr(attr xml.Attr) error {
	return s.parseString(attr.Value)
}

// 主解析方法，处理所有可能的格式
func (s *StArrayF) parseString(str string) error {
	str = strings.TrimSpace(str)
	if str == "" {
		*s = StArrayF{}
		return nil
	}

	// 统一解析逻辑
	return s.parseMixedSequence(str)
}

// 解析混合序列
func (s *StArrayF) parseMixedSequence(str string) error {
	var result StArrayF
	parts := strings.Fields(str)

	gFlag := false
	gCount := 0
	for _, p := range parts {
		if p == "g" {
			gFlag = true
			continue
		}
		if gFlag {
			gCount, _ = strconv.Atoi(p)
			gFlag = false
			continue
		}
		if gCount > 0 {
			v, _ := strconv.ParseFloat(p, 64)
			for j := 0; j < gCount; j++ {
				result = append(result, v)
			}
			gCount = 0
		} else {
			if v, err := strconv.ParseFloat(p, 64); err == nil {
				result = append(result, v)
			}
		}
	}

	*s = result
	return nil
}

// 转换为普通字符串
func (s StArrayF) String() string {
	if len(s) == 0 {
		return ""
	}

	strs := make([]string, len(s))
	for i, v := range s {
		strs[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(strs, " ")
}

// MarshalXML 实现 xml.Marshaler 接口
func (s StArrayF) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	content := s.String()
	return e.EncodeElement(content, start)
}

// MarshalXMLAttr 实现 xml.MarshalerAttr 接口
func (s StArrayF) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	content := s.String()
	return xml.Attr{
		Name:  name,
		Value: content,
	}, nil
}

type StArrayI []int

// UnmarshalXML 实现 xml.Unmarshaler 接口
func (s *StArrayI) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var content string
	if err := d.DecodeElement(&content, &start); err != nil {
		return err
	}
	return s.parseString(content)
}

// UnmarshalXMLAttr 实现 xml.UnmarshalerAttr 接口
func (s *StArrayI) UnmarshalXMLAttr(attr xml.Attr) error {
	return s.parseString(attr.Value)
}

// 主解析方法，处理所有可能的格式
func (s *StArrayI) parseString(str string) error {
	str = strings.TrimSpace(str)
	if str == "" {
		*s = StArrayI{}
		return nil
	}

	// 统一解析逻辑
	return s.parseMixedSequence(str)
}

// 解析混合序列
func (s *StArrayI) parseMixedSequence(str string) error {
	var result StArrayI
	parts := strings.Fields(str)
	for _, p := range parts {
		if v, err := strconv.Atoi(p); err == nil {
			result = append(result, v)
		}
	}

	*s = result
	return nil
}

// 转换为普通字符串
func (s StArrayI) String() string {
	if len(s) == 0 {
		return ""
	}

	strs := make([]string, len(s))
	for i, v := range s {
		strs[i] = strconv.Itoa(v)
	}
	return strings.Join(strs, " ")
}

// MarshalXML 实现 xml.Marshaler 接口
func (s StArrayI) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	content := s.String()
	return e.EncodeElement(content, start)
}

// MarshalXMLAttr 实现 xml.MarshalerAttr 接口
func (s StArrayI) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	content := s.String()
	return xml.Attr{
		Name:  name,
		Value: content,
	}, nil
}
