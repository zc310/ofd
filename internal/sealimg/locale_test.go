package sealimg

import "testing"

// TestIsEnglishTag 守住标签解析：POSIX 形式、BCP 47 形式都要认，C/POSIX 与
// 中文标签不能被误判成英文。
func TestIsEnglishTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"en_US.UTF-8", true},
		{"en_GB.UTF-8@euro", true},
		{"en", true},
		{"EN_us", true},
		{"en-US", true}, // Windows GetUserDefaultLocaleName 的形式
		{"en-GB", true},
		{" zh-CN ", false}, // Windows 中文系统
		{"zh_CN.UTF-8", false},
		{"zh-Hans-CN", false},
		{"C", false},
		{"POSIX", false},
		{"C.UTF-8", false},
		{"", false},
		{".UTF-8", false}, // 剥掉编码后缀后为空
		{"@euro", false},
		{"english", false},
		// grandfathered tag：手写前缀判断会漏掉，交给 x/text 规范化。
		{"en-GB-oed", true},
		{"en_US.UTF-8@piglatin", true}, // modifier 里的值不该被当语言
		{"EN-Latn-US", true},           // 带文字子标签
		{"en-GB", true},
		// und（未定义语言）不是英文。
		{"und", false},
		{"und-US", false},
		{"zxx", false}, // 无语言内容
	}
	for _, tc := range cases {
		if got := isEnglishTag(tc.tag); got != tc.want {
			t.Errorf("isEnglishTag(%q) = %v，期望 %v", tc.tag, got, tc.want)
		}
	}
}

// TestPosixLocaleToBCP47 守住 POSIX locale 名到 BCP 47 的转换：language.Parse
// 不认下划线与编码后缀，这一步漏了会让 en_US.UTF-8 直接解析失败。
func TestPosixLocaleToBCP47(t *testing.T) {
	cases := []struct{ in, want string }{
		{"en_US.UTF-8", "en-US"},
		{"en_GB.UTF-8@euro", "en-GB"},
		{"zh_CN.UTF-8", "zh-CN"},
		{"en-US", "en-US"},
		{" zh_CN ", "zh-CN"},
		{"C", "C"}, // 未设置语言，保持原样交给 Parse 兜底
		{"POSIX", "POSIX"},
		{".UTF-8", ""}, // 只有后缀
		{"@euro", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := posixLocaleToBCP47(tc.in); got != tc.want {
			t.Errorf("posixLocaleToBCP47(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}
