// Package localization 提供各工具共用的语言规范化逻辑。
package localization

import (
	"strings"

	"golang.org/x/text/language"
)

func CanonicalLanguage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "zh-CN"
	}
	tag, err := language.Parse(value)
	if err != nil {
		return "zh-CN"
	}
	base, _, _ := tag.Raw()
	englishBase, _, _ := language.English.Raw()
	if base == englishBase {
		return "en"
	}
	return "zh-CN"
}
