package models

import (
	"encoding/xml"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type StArrayF []float64

const (
	// stArrayFMaxRepeat 限制单个 g 指令的展开数量，避免异常输入导致长时间分配。
	stArrayFMaxRepeat = 100000
	// stArrayFMaxElements 限制数组展开后的总元素数量，避免多个 g 指令累积耗尽内存。
	stArrayFMaxElements = 1000000
)

// scanFields 保存前 len(fields) 个字段并返回总数，沿用 strings.Fields 的 Unicode
// 空白规则，不分配中间切片。fields 为 nil 时只计数。
func scanFields(str string, fields []string) int {
	count := 0
	for part := range strings.FieldsSeq(str) {
		if count < len(fields) {
			fields[count] = part
		}
		count++
	}
	return count
}

var asciiFieldSpace = [256]byte{'\t': 1, '\n': 1, '\v': 1, '\f': 1, '\r': 1, ' ': 1}

// countFields 为常见的 ASCII 数值序列计数；Unicode 输入仍按 Fields 的规则处理。
func countFields(str string) int {
	count, wasSpace := 0, 1
	var mask byte
	for i := 0; i < len(str); i++ {
		c := str[i]
		mask |= c
		space := int(asciiFieldSpace[c])
		count += wasSpace &^ space
		wasSpace = space
	}
	if mask >= utf8.RuneSelf {
		return scanFields(str, nil)
	}
	return count
}

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
	// 绝大多数输入是不含 g 展开标记的浮点序列（DashPattern、DeltaX/DeltaY 等）。
	// 走快速路径：预分配结果并按 token 数一次写满，避免 append 扩容。
	if strings.IndexByte(str, 'g') < 0 {
		return s.parsePlainSequence(str)
	}

	var result StArrayF

	gFlag := false
	hasRepeat := false
	repeatCount := 0
	for p := range strings.FieldsSeq(str) {
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
		v, err := parseFiniteFloat(p, "数组元素")
		if err != nil {
			return fmt.Errorf("无效的浮点数 %q: %w", p, err)
		}
		if hasRepeat {
			if len(result)+repeatCount > stArrayFMaxElements {
				return fmt.Errorf("数组元素数量超过上限 %d", stArrayFMaxElements)
			}
			start := len(result)
			result = slices.Grow(result, repeatCount)
			result = result[:start+repeatCount]
			for j := start; j < len(result); j++ {
				result[j] = v
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

// parsePlainSequence 先计数再写入结果，只分配数值切片，不保留字段切片。
func (s *StArrayF) parsePlainSequence(str string) error {
	count := countFields(str)
	if count > stArrayFMaxElements {
		return fmt.Errorf("数组元素数量超过上限 %d", stArrayFMaxElements)
	}
	result := make(StArrayF, count)
	i := 0
	for p := range strings.FieldsSeq(str) {
		v, err := parseFiniteFloat(p, "数组元素")
		if err != nil {
			return fmt.Errorf("无效的浮点数 %q: %w", p, err)
		}
		result[i] = v
		i++
	}
	*s = result
	return nil
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
	// 预分配：token 数是结果元素数的上界（非法 token 会被跳过），避免 append 扩容。
	result := make(StArrayI, 0, countFields(str))
	for p := range strings.FieldsSeq(str) {
		if v, err := strconv.Atoi(p); err == nil {
			result = append(result, v)
		}
	}

	*s = result
	return nil
}
