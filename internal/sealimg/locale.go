package sealimg

import (
	"strings"

	"golang.org/x/text/language"
)

// englishBase 是英文的基础语言标签。判断时只比基础语言：地区与文字子标签
// （en-GB、en-Latn-US）都不影响「是不是英文」这个问题的答案。
var englishBase, _, _ = language.English.Raw()

// isEnglishTag 判断一个语言标签是否指示英文。
//
// 同时接受 POSIX 形式（en_US.UTF-8、en_GB.UTF-8@euro）和 Windows / BCP 47 形式
// （en-US、en-GB）。后者是 Windows GetUserDefaultLocaleName 的原生返回。
//
// 匹配交给 golang.org/x/text/language，好处是标准库认识 grandfathered tag：
// en-GB-oed 会被规范成 en-GB-oxendict 而不是被判成未知语言。C、POSIX、空串、
// english 这类无效输入解析后统一落到 und，不等于 english，无需逐个列举排除。
func isEnglishTag(tag string) bool {
	tag = posixLocaleToBCP47(tag)
	if tag == "" {
		return false
	}
	parsed, err := language.Parse(tag)
	if err != nil {
		return false
	}
	base, _, _ := parsed.Raw()
	return base == englishBase
}

// posixLocaleToBCP47 把 POSIX locale 名转成 BCP 47 标签：分隔符 _ 换成 -，剥掉
// .UTF-8 之类的编码后缀与 @modifier 修饰符。
//
// 这一步省不掉：language.Parse 不认下划线，也不接受编码后缀，
// language.Parse("en_US.UTF-8") 与 Parse("zh_CN.UTF-8") 都直接返回错误。
// C 与 POSIX 表示「未设置语言」，转换后仍是 C，交给 Parse 兜底即可。
func posixLocaleToBCP47(tag string) string {
	tag = strings.TrimSpace(tag)
	if index := strings.IndexAny(tag, ".@"); index >= 0 {
		tag = tag[:index]
	}
	return strings.ReplaceAll(tag, "_", "-")
}
