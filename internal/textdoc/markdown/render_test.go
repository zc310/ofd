package markdown

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/textdoc"
)

func TestEscapeLinePreservesLiteralMarkdown(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"", ""}, {"plain text", "plain text"}, {"# heading", `\# heading`},
		{"- item", `\- item`}, {"+ item", `\+ item`}, {"= title", `\= title`},
		{"12. item", `12\. item`}, {"abc. text", "abc. text"},
		{`a\b|c`, `a\\b\|c`}, {"`code` *bold* _word_ [link] <tag> ~x~", "\\`code\\` \\*bold\\* \\_word\\_ \\[link\\] \\<tag\\> \\~x\\~"},
	} {
		if got := EscapeLine(test.input); got != test.want {
			t.Errorf("EscapeLine(%q)=%q，期望 %q", test.input, got, test.want)
		}
	}
}

func TestRenderPageRendersHeadingsParagraphsAndSkipsFooter(t *testing.T) {
	entries := []textdoc.Entry{
		{Text: "Overview", X: 10, Y: 20, Size: 12, Width: 60},
		{Text: "first body.", X: 10, Y: 40, Size: 6, Width: 90},
		{Text: "second body.", X: 10, Y: 50, Size: 6, Width: 90},
		{Text: "indented body.", X: 18, Y: 60, Size: 6, Width: 82},
		{Text: "12", X: 50, Y: 95, Size: 6, Width: 5},
	}
	var output strings.Builder
	RenderPage(&output, entries, 100, false)
	want := "\n### Overview\n\nfirst body.\nsecond body.\n\nindented body.\n"
	if output.String() != want {
		t.Fatalf("Markdown 输出=%q，期望 %q", output.String(), want)
	}
}

func TestRenderPageChapterAndHeadingNormalization(t *testing.T) {
	var output strings.Builder
	RenderPage(&output, []textdoc.Entry{
		{Text: "第一章 概述", X: 10, Y: 10, Size: 6, Width: 50},
		{Text: "Overview", X: 10, Y: 30, Size: 9, Width: 50},
		{Text: "body.", X: 10, Y: 50, Size: 6, Width: 50},
		{Text: "body continued.", X: 10, Y: 60, Size: 6, Width: 50},
	}, 200, false)
	want := "\n## 第一章 概述\n### Overview\n\nbody.\nbody continued.\n"
	if output.String() != want {
		t.Fatalf("章节或标题归一化错误: %q，期望 %q", output.String(), want)
	}
	output.Reset()
	RenderPage(&output, nil, 100, true)
	RenderPage(&output, []textdoc.Entry{{Text: "12", Y: 95, Size: 6}}, 100, false)
	if output.Len() != 0 {
		t.Fatalf("空页或仅有页码的页面不应输出内容: %q", output.String())
	}
}

func TestRenderPageDetectsTablesOnlyWhenEnabled(t *testing.T) {
	entries := []textdoc.Entry{
		{Text: "名称", X: 10, Y: 20, Size: 5, Width: 10}, {Text: "数量", X: 50, Y: 20, Size: 5, Width: 10},
		{Text: "项目", X: 10, Y: 30, Size: 5, Width: 10}, {Text: "12", X: 50, Y: 30, Size: 5, Width: 10},
	}
	var output strings.Builder
	RenderPage(&output, entries, 200, true)
	want := "\n| 名称 | 数量 |\n| --- | --- |\n| 项目 | 12 |\n\n"
	if output.String() != want {
		t.Fatalf("识别的表格=%q，期望 %q", output.String(), want)
	}
	output.Reset()
	RenderPage(&output, entries, 200, false)
	if strings.Contains(output.String(), "| --- |") || !strings.Contains(output.String(), "12") {
		t.Fatalf("关闭表格识别后应保留普通文字: %q", output.String())
	}
}

func TestRenderTableEscapesCellsAndPadsShortRows(t *testing.T) {
	var output strings.Builder
	RenderTable(&output, textdoc.Table{Header: []string{" A|B ", "路径"}, Rows: [][]string{{"line\nnext\r", `C:\tmp`}, {"only one"}, {"left", "right", "ignored"}}})
	want := "\n| A\\|B | 路径 |\n| --- | --- |\n| line next | C:\\\\tmp |\n| only one |  |\n| left | right |\n\n"
	if output.String() != want {
		t.Fatalf("表格转义或列数处理错误: %q，期望 %q", output.String(), want)
	}
	output.Reset()
	RenderTable(&output, textdoc.Table{Rows: [][]string{{"orphan"}}})
	if output.Len() != 0 {
		t.Fatal("无表头时不应输出表格")
	}
}
