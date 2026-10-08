package markdown

import "testing"

// goldmark 的行段 Value 自带换行符。codeText 若再补一个 '\n'，每行之间会多出
// 空行，代码块高度翻倍，行内代码的纵向对齐也会跟着错位。
func TestCodeTextKeepsOneNewlinePerLine(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"围栏代码块", "```\na\nb\nc\n```\n", "a\nb\nc"},
		{"围栏内保留空行", "```\na\n\nb\n```\n", "a\n\nb"},
		{"缩进代码块", "    a\n    b\n", "a\nb"},
		{"带语言标记", "```go\nfmt.Println(1)\n```\n", "fmt.Println(1)"},
		{"末尾多空行", "```\na\n\n\n```\n", "a"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document, err := parseMarkdown([]byte(testCase.source), "")
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(document.Blocks) != 1 {
				t.Fatalf("块数不符预期: %d", len(document.Blocks))
			}
			if got := document.Blocks[0].Code; got != testCase.want {
				t.Fatalf("代码文本 = %q, 期望 %q", got, testCase.want)
			}
		})
	}
}

// 代码块文本不得被转义或改写：制表符要留给排版层展开。
func TestCodeTextPreservesTabs(t *testing.T) {
	document, err := parseMarkdown([]byte("```\n\tif x {\n\t\treturn\n\t}\n```\n"), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := "\tif x {\n\t\treturn\n\t}"
	if got := document.Blocks[0].Code; got != want {
		t.Fatalf("代码文本 = %q, 期望 %q", got, want)
	}
}
