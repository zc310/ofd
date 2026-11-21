package flow

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// ValidateText 检查 UTF-8 与控制字符，供公开创作入口与清单入口复用。
func ValidateText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("文字不是有效 UTF-8")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return fmt.Errorf("不支持的控制字符 U+%04X", r)
		}
	}
	return nil
}
