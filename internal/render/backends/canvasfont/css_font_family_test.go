package canvasfont

import "testing"

// canvas 的 SVG 输出把字体族名不加引号拼进 font 简写，族名里的 "+"（OFD 内嵌字体
// 常见的子集前缀）会让整条 font 声明作废，浏览器退回默认字号。规范成合法 CSS 标识符
// 后声明才生效。
func TestCSSFontFamilyName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"子集前缀加号替换", "BCCDEE+FZHTK--GBK1-0", "BCCDEE-FZHTK--GBK1-0"},
		{"普通族名不变", "default", "default"},
		{"空格保留", "Times New Roman", "Times New Roman"},
		{"非 ASCII 保留", "宋体", "宋体"},
		{"其他非法字符替换", "A.B,C", "A-B-C"},
		{"数字开头补前缀", "0abc", "f0abc"},
		{"空串原样返回", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cssFontFamilyName(tc.in); got != tc.want {
				t.Errorf("cssFontFamilyName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
