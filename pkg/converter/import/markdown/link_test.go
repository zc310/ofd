package markdown

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/layout"
)

// inlineHrefs 收集文档里所有非空的点击目标，按出现顺序排列。
func inlineHrefs(document *layout.Document) []string {
	var hrefs []string
	collect := func(inlines []layout.Inline) {
		for _, in := range inlines {
			if in.Href != "" {
				hrefs = append(hrefs, in.Href)
			}
		}
	}
	for _, block := range document.Blocks {
		collect(block.Inlines)
		if block.Table != nil {
			for _, cell := range block.Table.Header {
				collect(cell)
			}
			for _, row := range block.Table.Rows {
				for _, cell := range row {
					collect(cell)
				}
			}
		}
	}
	return hrefs
}

// Markdown 的链接目标此前在解析那一步就被丢掉，只留下一个染色标记，生成的 OFD 里
// 链接是蓝字但点不动。目标必须一路传到图元的 CLICK 动作上。
func TestParseMarkdownKeepsLinkHref(t *testing.T) {
	document, err := parseMarkdown([]byte(`参见 [项目主页](https://github.com/zc310/ofd) 与
<https://example.net>。

| 名称 | 链接 |
| --- | --- |
| 示例 | [表格](https://example.com/tbl) |

- 列表里的 [条目](https://example.com/li)
`), "test.md")
	if err != nil {
		t.Fatal(err)
	}
	hrefs := inlineHrefs(document)
	want := []string{
		"https://github.com/zc310/ofd",
		"https://example.net",
		"https://example.com/tbl",
		"https://example.com/li",
	}
	if len(hrefs) != len(want) {
		t.Fatalf("点击目标 = %v, want %v", hrefs, want)
	}
	for index := range want {
		if hrefs[index] != want[index] {
			t.Errorf("第 %d 个点击目标 = %q, want %q", index+1, hrefs[index], want[index])
		}
	}
}

// 空目标与纯锚点不生成点击目标：锚点指向 Markdown 标题，而标题没有被写成 OFD
// 大纲，挂上去只会得到一个点了没反应的热区。两者仍要按链接着色。
func TestParseMarkdownSkipsEmptyAndAnchorHrefs(t *testing.T) {
	document, err := parseMarkdown([]byte(`[空目标]() 与 [锚点](#第一节)。
`), "test.md")
	if err != nil {
		t.Fatal(err)
	}
	if hrefs := inlineHrefs(document); len(hrefs) != 0 {
		t.Fatalf("空目标与锚点不应生成点击目标，实际 %v", hrefs)
	}
	var linked int
	for _, block := range document.Blocks {
		for _, in := range block.Inlines {
			if in.Link {
				linked++
			}
		}
	}
	if linked == 0 {
		t.Error("空目标与锚点仍应标记为链接以便着色")
	}
}

// 相对链接原样保留，不拿 Markdown 所在目录拼成绝对路径：文档可能被单独分发，
// 把本机路径写进产物既不可移植也泄漏目录结构。
func TestParseMarkdownKeepsRelativeHref(t *testing.T) {
	document, err := parseMarkdown([]byte(`[相对](docs/guide.md)`), "test.md")
	if err != nil {
		t.Fatal(err)
	}
	hrefs := inlineHrefs(document)
	if len(hrefs) != 1 || hrefs[0] != "docs/guide.md" {
		t.Fatalf("相对链接应原样保留，实际 %v", hrefs)
	}
	if strings.Contains(hrefs[0], "/tmp") || strings.HasPrefix(hrefs[0], "/") {
		t.Errorf("相对链接不应被拼成绝对路径，实际 %q", hrefs[0])
	}
}

// 行内代码里的链接语法不是链接，不能挂动作。
func TestParseMarkdownIgnoresLinkInsideCode(t *testing.T) {
	document, err := parseMarkdown([]byte("`[x](https://example.com)` 与 [真链接](https://example.org)"), "test.md")
	if err != nil {
		t.Fatal(err)
	}
	hrefs := inlineHrefs(document)
	if len(hrefs) != 1 || hrefs[0] != "https://example.org" {
		t.Fatalf("只有真正的链接应有目标，实际 %v", hrefs)
	}
}
