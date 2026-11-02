package models

import (
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	errStArrayMaxRepeat   = errors.New("ST_Array展开次数超出上限")
	errStArrayMaxElements = errors.New("ST_Array元素数量超过上限")
	errStArrayUnsupported = errors.New("不支持的 ST_Array 元素类型")
	errStArrayRepeatMark  = errors.New("重复展开标记 g 缺少重复次数")
	errStArrayRepeatValue = errors.New("展开次数后缺少重复值")
	errStArrayRepeatCount = errors.New("展开标记 g 后缺少重复次数")
)

// StArray 是 OFD ST_Array 数值数组；T 按具体字段选择浮点值或整型值。
type StArray[T float64 | int] []T

const (
	// stArrayMaxRepeat 限制单个 g 指令的展开数量，避免异常输入导致长时间分配。
	stArrayMaxRepeat = 100000
	// stArrayMaxElements 限制数组展开后的总元素数量，避免多个 g 指令累积耗尽内存。
	stArrayMaxElements = 1000000
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
func (s *StArray[T]) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var content string
	if err := d.DecodeElement(&content, &start); err != nil {
		return err
	}
	return s.parseString(content)
}

// UnmarshalXMLAttr 实现 xml.UnmarshalerAttr 接口
func (s *StArray[T]) UnmarshalXMLAttr(attr xml.Attr) error {
	return s.parseString(attr.Value)
}

// 主解析方法，处理所有可能的格式
func (s *StArray[T]) parseString(str string) error {
	str = strings.TrimSpace(str)
	if str == "" {
		*s = StArray[T]{}
		return nil
	}
	switch any(T(0)).(type) {
	case float64:
		values, err := parseFloatArray[T](str)
		if err != nil {
			return err
		}
		*s = values
	case int:
		*s = parseIntegerArray[T](str)
	default:
		return errStArrayUnsupported
	}
	return nil
}

func parseFloatArray[T float64 | int](str string) (StArray[T], error) {
	// 绝大多数输入是不含 g 展开标记的浮点序列（DashPattern、DeltaX/DeltaY 等）。
	// 走快速路径：预分配结果并按 token 数一次写满，避免 append 扩容。
	if strings.IndexByte(str, 'g') < 0 {
		return parsePlainFloatSequence[T](str)
	}

	var result StArray[T]

	gFlag := false
	hasRepeat := false
	repeatCount := 0
	for p := range strings.FieldsSeq(str) {
		if p == "g" {
			if gFlag {
				return nil, errStArrayRepeatMark
			}
			if hasRepeat {
				return nil, errStArrayRepeatValue
			}
			gFlag = true
			continue
		}
		if gFlag {
			count, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("无效的展开次数 %q: %w", p, err)
			}
			if count < 0 || count > stArrayMaxRepeat {
				return nil, errStArrayMaxRepeat
			}
			repeatCount = count
			hasRepeat = true
			gFlag = false
			continue
		}
		v, err := parseFiniteFloat(p, "数组元素")
		if err != nil {
			return nil, fmt.Errorf("无效的浮点数 %q: %w", p, err)
		}
		if hasRepeat {
			if len(result)+repeatCount > stArrayMaxElements {
				return nil, errStArrayMaxElements
			}
			start := len(result)
			result = slices.Grow(result, repeatCount)
			result = result[:start+repeatCount]
			for j := start; j < len(result); j++ {
				result[j] = T(v)
			}
			hasRepeat = false
		} else {
			if len(result) >= stArrayMaxElements {
				return nil, errStArrayMaxElements
			}
			result = append(result, T(v))
		}
		if len(result) > stArrayMaxElements {
			return nil, errStArrayMaxElements
		}
	}
	if gFlag {
		return nil, errStArrayRepeatCount
	}
	return result, nil
}

// parsePlainSequence 先计数再写入结果，只分配数值切片，不保留字段切片。
func parsePlainFloatSequence[T float64 | int](str string) (StArray[T], error) {
	count := countFields(str)
	if count > stArrayMaxElements {
		return nil, errStArrayMaxElements
	}
	result := make(StArray[T], count)
	i := 0
	for p := range strings.FieldsSeq(str) {
		v, err := parseFiniteFloat(p, "数组元素")
		if err != nil {
			return nil, fmt.Errorf("无效的浮点数 %q: %w", p, err)
		}
		result[i] = T(v)
		i++
	}
	return result, nil
}

func parseIntegerArray[T float64 | int](str string) StArray[T] {
	// 预分配：token 数是结果元素数的上界（非法 token 会被跳过），避免 append 扩容。
	result := make(StArray[T], 0, countFields(str))
	for p := range strings.FieldsSeq(str) {
		if v, err := strconv.Atoi(p); err == nil {
			result = append(result, T(v))
		}
	}

	return result
}
