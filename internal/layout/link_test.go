package layout

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

// hrefOf 返回文字对象上的点击目标，没有动作时返回空串。
func hrefOf(text creator.Text) string {
	for _, action := range text.Actions {
		if action.URI != nil {
			return action.URI.URI
		}
	}
	return ""
}

// sameColor 比较两个颜色的 RGB 分量，忽略颜色空间分量。
func sameColor(a, b *creator.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.R == b.R && a.G == b.G && a.B == b.B
}

// 链接文字要挂上 CLICK 动作，点击目标与字符级样式一起下沉到图元。
//
// 此前 layout 只按链接给文字染色，URL 在 Markdown 解析那一步就被丢了，生成的
// OFD 里链接只是蓝字，点不动。
func TestBuildAttachesLinkActionToText(t *testing.T) {
	options := DefaultOptions()
	options.PageWidth = 120
	options.PageHeight = 80
	options.MarginLeft = 10
	options.MarginRight = 10
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindParagraph,
		Inlines: []Inline{
			{Text: "访问 "},
			{Text: "项目主页", Link: true, Href: "https://github.com/zc310/ofd"},
			{Text: " 结束"},
		},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var linked int
	for _, text := range textItems(document) {
		if !strings.Contains(text.Value, "项目主页") {
			continue
		}
		linked++
		if got := hrefOf(text); got != "https://github.com/zc310/ofd" {
			t.Errorf("链接文字的点击目标 = %q, want 项目主页地址", got)
		}
		if len(text.Actions) > 0 && text.Actions[0].Event != creator.ActionEventClick {
			t.Errorf("动作事件 = %q, want CLICK", text.Actions[0].Event)
		}
	}
	if linked == 0 {
		t.Fatal("没有找到链接文字对象")
	}
	// 非链接文字不能带动作。
	for _, text := range textItems(document) {
		if strings.Contains(text.Value, "访问") && len(text.Actions) != 0 {
			t.Errorf("普通文字 %q 不应带动作，实际 %+v", text.Value, text.Actions)
		}
	}
}

// 相邻的两个不同链接不能并成一个文字对象，否则后一个链接会挂上前一个的地址。
//
// mergeRuns 合并相邻同样式片段的判据里必须带上 href——颜色、字号、字重都相同，
// 只有 URL 不同。
func TestBuildKeepsAdjacentLinksSeparate(t *testing.T) {
	options := DefaultOptions()
	options.PageWidth = 160
	options.PageHeight = 80
	options.MarginLeft = 10
	options.MarginRight = 10
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindParagraph,
		Inlines: []Inline{
			{Text: "甲", Link: true, Href: "https://a.example"},
			{Text: "乙", Link: true, Href: "https://b.example"},
		},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	got := map[string]bool{}
	for _, text := range textItems(document) {
		got[hrefOf(text)] = true
	}
	if !got["https://a.example"] {
		t.Error("第一个链接的目标丢失")
	}
	if !got["https://b.example"] {
		t.Error("第二个链接被并进了第一个，只挂上了前一个地址")
	}
}

// 空目标只染色不挂动作：挂一条空 URI 的动作会被创建器判为非法，整份文档都生成不出来。
func TestBuildSkipsActionForEmptyHref(t *testing.T) {
	options := DefaultOptions()
	options.PageWidth = 120
	options.PageHeight = 80
	options.MarginLeft = 10
	options.MarginRight = 10
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "空目标", Link: true}},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	for _, text := range textItems(document) {
		if len(text.Actions) != 0 {
			t.Errorf("空目标链接不应带动作，实际 %+v", text.Actions)
		}
		if !sameColor(text.FillColor, colorLink) {
			t.Errorf("空目标链接仍应按链接着色，实际 %+v", text.FillColor)
		}
	}
}

// 表格单元格里的链接同样要挂上动作：纵向表格走 addText，横排表格直接构造
// creator.Text，两条路都得覆盖。
func TestBuildLinksInsideTableCell(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindTable,
		Table: &Table{
			Header: []Cell{{{Text: "名称"}}, {{Text: "链接"}}},
			Rows:   [][]Cell{{{{Text: "示例"}}, {{Text: "站点", Link: true, Href: "https://example.com"}}}},
			Align:  []Align{AlignLeft, AlignLeft},
		},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	found := false
	for _, text := range textItems(document) {
		if hrefOf(text) == "https://example.com" {
			found = true
		}
	}
	if !found {
		t.Error("表格单元格里的链接没有挂上动作")
	}
}
