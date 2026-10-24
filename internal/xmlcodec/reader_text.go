package xmlcodec

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// xmlText 一次扫描同时完成字符引用解码和字面换行归一化。
// 字符引用中的 CR 不参与归一化。
func xmlText(raw []byte) (string, bool) {
	return decodeXMLText(raw, false)
}

// XML 1.0 §2.11、§3.3.3：属性中的字面空白变为空格；字符引用保持原值。
func xmlAttribute(raw []byte) (string, bool) {
	return decodeXMLText(raw, true)
}

func decodeXMLText(raw []byte, attribute bool) (string, bool) {
	seek := "&\r"
	if attribute {
		seek = "&\r\t\n"
	}
	index := bytes.IndexAny(raw, seek)
	if index < 0 {
		return string(raw), true
	}
	var dst strings.Builder
	dst.Grow(len(raw))
	for index >= 0 {
		dst.Write(raw[:index])
		raw = raw[index:]
		if raw[0] == '\r' {
			if attribute {
				dst.WriteByte(' ')
			} else {
				dst.WriteByte('\n')
			}
			raw = raw[1:]
			if len(raw) > 0 && raw[0] == '\n' {
				raw = raw[1:]
			}
		} else if raw[0] == '\t' || raw[0] == '\n' {
			dst.WriteByte(' ')
			raw = raw[1:]
		} else {
			end := bytes.IndexByte(raw, ';')
			if end < 0 {
				return "", false
			}
			r, ok := xmlReference(raw[1:end])
			if !ok {
				return "", false
			}
			dst.WriteRune(r)
			raw = raw[end+1:]
		}
		index = bytes.IndexAny(raw, seek)
	}
	dst.Write(raw)
	return dst.String(), true
}

func xmlReference(entity []byte) (rune, bool) {
	switch string(entity) {
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	if len(entity) < 2 || entity[0] != '#' {
		return 0, false
	}
	digits, base := entity[1:], 10
	if digits[0] == 'x' {
		digits, base = digits[1:], 16
	}
	if len(digits) == 0 || digits[0] == '+' || digits[0] == '-' {
		return 0, false
	}
	value, err := strconv.ParseUint(string(digits), base, 32)
	return rune(value), err == nil && value <= utf8.MaxRune && isXMLChar(rune(value))
}

func xmlLines(raw []byte) string {
	if bytes.IndexByte(raw, '\r') < 0 {
		return string(raw)
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
}
