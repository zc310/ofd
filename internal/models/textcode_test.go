package models

import "testing"

func TestDecodeTextEscapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空格转义", `\0020`, " "},
		{"无转义原样返回", "海关总署", "海关总署"},
		{"转义与普通字符混合", `目\0020\0020次`, "目  次"},
		{"反斜杠转义", `\005C`, `\`},
		{"反斜杠不是四位十六进制时按字面保留", `\\`, `\\`},
		{"十六进制位数不足", `\00`, `\00`},
		{"非法十六进制字符", `\00G0`, `\00G0`},
		{"小写十六进制", `\00e9`, "é"},
		{"代理对合并为一个码点", `\D83D\DE00`, "😀"},
		{"孤立高位代理降级为替换符", `\D83D`, "\uFFFD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeTextEscapes(tc.in); got != tc.want {
				t.Errorf("decodeTextEscapes(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// 绝大多数 TextCode 不含转义，这条应当零分配、只做一次字节扫描。
func BenchmarkDecodeTextEscapesNoEscape(b *testing.B) {
	const text = "国家税务总局关于增值税发票开具事项的公告"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = decodeTextEscapes(text)
	}
}

func BenchmarkDecodeTextEscapesWithEscapes(b *testing.B) {
	const text = `目\0020\0020次\005C普通文字\0020结尾`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = decodeTextEscapes(text)
	}
}
