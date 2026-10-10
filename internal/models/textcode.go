package models

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// decodeTextEscapes 解码 TextCode 文本里的 OFD 转义序列。OFD 允许用反斜杠加四位
// 十六进制数表示一个 UTF-16 码元，最典型的是空格写作 "\0020"——XML 内容里直接写
// 空格容易被生产方裁剪或解析链路规整掉，因此转义书写。连续的高低代理码元合并成
// 一个码点；反斜杠后面不是四位十六进制时按字面反斜杠保留。
//
// 转义只用 ASCII 字符，非转义内容按原字节拷贝，因此按字节扫描即可，不必把整串转成
// []rune。绝大多数文本不含转义，IndexByte 命中失败直接原样返回。
func decodeTextEscapes(text string) string {
	if strings.IndexByte(text, '\\') < 0 {
		return text
	}
	// "\xxxx" 是 5 个字节，最多解码成一个 4 字节的 UTF-8 序列，输出不会比输入长。
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		c := text[i]
		if c != '\\' || i+4 >= len(text) {
			out = append(out, c)
			i++
			continue
		}
		unit, ok := hexCodeUnit(text[i+1], text[i+2], text[i+3], text[i+4])
		if !ok {
			out = append(out, c)
			i++
			continue
		}
		i += 5
		if utf16.IsSurrogate(rune(unit)) {
			if i+5 <= len(text) && text[i] == '\\' {
				if low, ok := hexCodeUnit(text[i+1], text[i+2], text[i+3], text[i+4]); ok {
					if combined := utf16.DecodeRune(rune(unit), rune(low)); combined != unicode.ReplacementChar {
						out = utf8.AppendRune(out, combined)
						i += 5
						continue
					}
				}
			}
			out = utf8.AppendRune(out, unicode.ReplacementChar)
			continue
		}
		out = utf8.AppendRune(out, rune(unit))
	}
	return string(out)
}

// hexCodeUnit 解析一个四位十六进制码元，任意一位不是十六进制时返回 false。
func hexCodeUnit(a, b, c, d byte) (uint16, bool) {
	va, ok := hexDigit(a)
	if !ok {
		return 0, false
	}
	vb, ok := hexDigit(b)
	if !ok {
		return 0, false
	}
	vc, ok := hexDigit(c)
	if !ok {
		return 0, false
	}
	vd, ok := hexDigit(d)
	if !ok {
		return 0, false
	}
	return uint16(va)<<12 | uint16(vb)<<8 | uint16(vc)<<4 | uint16(vd), true
}

func hexDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
