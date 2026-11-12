package markdown

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

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
			if got := document.Blocks[0].Text; got != testCase.want {
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
	if got := document.Blocks[0].Text; got != want {
		t.Fatalf("代码文本 = %q, 期望 %q", got, want)
	}
}

func TestMarkdownCodeHighlightsOnlyKnownDeclaredLanguages(t *testing.T) {
	for _, language := range []string{"go", "golang", "python", "js", "", "unknown-ofd-language", "text"} {
		t.Run(language, func(t *testing.T) {
			source := "```" + language + "\nreturn 42\nvalue = \"hello\"\n```\n"
			doc, err := parseMarkdown([]byte(source), "", flow.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			model, err := doc.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			fonts := make(map[string]creator.Font)
			for _, font := range model.Fonts {
				fonts[font.Name] = font
			}
			colors := make(map[[3]uint8]bool)
			for _, item := range model.Pages[0].Items {
				text, ok := item.(creator.Text)
				if !ok {
					continue
				}
				var value strings.Builder
				for _, code := range text.TextCodes {
					value.WriteString(code.Value)
				}
				if value.String() == language {
					continue // 首段的语言标签不参与词元着色断言。
				}
				if !fonts[text.Font].FixedWidth || text.FillColor == nil {
					t.Fatalf("高亮丢失等宽字体或颜色: %+v", text)
				}
				c := text.FillColor
				colors[[3]uint8{c.R, c.G, c.B}] = true
			}
			switch language {
			case "", "unknown-ofd-language", "text":
				if len(colors) != 1 || !colors[[3]uint8{0x57, 0x60, 0x6a}] {
					t.Fatalf("无标记或未知语言应默认灰字: %+v", colors)
				}
			default:
				if len(colors) < 3 {
					t.Fatalf("已知语言缺少语法高亮: %+v", colors)
				}
			}
		})
	}
}
