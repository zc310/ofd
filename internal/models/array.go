package models

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type StArrayF []float64

const (
	// stArrayFMaxRepeat 限制单个 g 指令的展开数量，避免异常输入导致长时间分配。
	stArrayFMaxRepeat = 100000
	// stArrayFMaxElements 限制数组展开后的总元素数量，避免多个 g 指令累积耗尽内存。
	stArrayFMaxElements = 1000000
)

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
	hasRepeat := false
	repeatCount := 0
	for _, p := range parts {
		if p == "g" {
			if gFlag {
				return fmt.Errorf("重复展开标记 g 缺少重复次数")
			}
			if hasRepeat {
				return fmt.Errorf("展开次数后缺少重复值")
			}
			gFlag = true
			continue
		}
		if gFlag {
			count, err := strconv.Atoi(p)
			if err != nil {
				return fmt.Errorf("无效的展开次数 %q: %w", p, err)
			}
			if count < 0 || count > stArrayFMaxRepeat {
				return fmt.Errorf("展开次数 %d 超出允许范围 [0,%d]", count, stArrayFMaxRepeat)
			}
			repeatCount = count
			hasRepeat = true
			gFlag = false
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return fmt.Errorf("无效的浮点数 %q: %w", p, err)
		}
		if hasRepeat {
			if len(result)+repeatCount > stArrayFMaxElements {
				return fmt.Errorf("数组元素数量超过上限 %d", stArrayFMaxElements)
			}
			for j := 0; j < repeatCount; j++ {
				result = append(result, v)
			}
			hasRepeat = false
		} else {
			if len(result) >= stArrayFMaxElements {
				return fmt.Errorf("数组元素数量超过上限 %d", stArrayFMaxElements)
			}
			result = append(result, v)
		}
		if len(result) > stArrayFMaxElements {
			return fmt.Errorf("数组元素数量超过上限 %d", stArrayFMaxElements)
		}
	}
	if gFlag {
		return fmt.Errorf("展开标记 g 后缺少重复次数")
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
