package converter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func tablePage(items ...models.PageItem) *parser.Document {
	return &parser.Document{Pages: []*parser.Page{parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{Items: items}}}},
	})}}
}

func TestMarkdownDetectsBorderlessTable(t *testing.T) {
	// 无边框表格：列由文字的 X 位置和空白间隔决定，第一行作为表头。
	doc := tablePage(
		textItemAt("序号", true, 30, 52, 5.2),
		textItemAt("编号", true, 49, 52, 5.2),
		textItemAt("项目名称", true, 83, 52, 5.2),
		textItemAt("申报地区或单位", true, 133, 52, 5.2),
		textItemAt("158", true, 30, 63, 5.2),
		textItemAt("桒-14", true, 47, 63, 5.2),
		textItemAt("浦东山歌", true, 83, 63, 5.2),
		textItemAt("浦东新区", true, 141, 63, 5.2),
		textItemAt("159", true, 30, 74, 5.2),
		textItemAt("桒-15", true, 47, 74, 5.2),
		textItemAt("上海工人大锣鼓", true, 75, 74, 5.2),
		textItemAt("杨浦区", true, 144, 74, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output, WithMarkdownTables(true)); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	want := "| 序号 | 编号 | 项目名称 | 申报地区或单位 |\n" +
		"| --- | --- | --- | --- |\n" +
		"| 158 | 桒-14 | 浦东山歌 | 浦东新区 |\n" +
		"| 159 | 桒-15 | 上海工人大锣鼓 | 杨浦区 |\n"
	if !strings.Contains(got, want) {
		t.Fatalf("markdown table missing.\n got: %q\nwant: %q", got, want)
	}
}

func TestMarkdownTableMergesMultilineCells(t *testing.T) {
	// 单元格内容跨多行时，续行按纵向位置合并到最近的表格行。
	doc := tablePage(
		textItemAt("序号", true, 30, 52, 5.2),
		textItemAt("名称", true, 83, 52, 5.2),
		textItemAt("单位", true, 133, 52, 5.2),
		textItemAt("收藏协会", true, 133, 64, 5.2),
		textItemAt("1", true, 30, 70, 5.2),
		textItemAt("古琴", true, 83, 70, 5.2),
		textItemAt("上海", true, 133, 70, 5.2),
		textItemAt("基金会", true, 133, 76, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output, WithMarkdownTables(true)); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "| 1 | 古琴 | 收藏协会上海基金会 |") {
		t.Fatalf("multiline cell not merged in order: %q", got)
	}
}

func TestMarkdownDoesNotTreatParagraphsAsTable(t *testing.T) {
	// 单列段落不应被识别为表格。
	doc := tablePage(
		textItemAt("第一段正文内容", true, 30, 52, 5.2),
		textItemAt("第二段正文内容", true, 30, 63, 5.2),
		textItemAt("第三段正文内容", true, 30, 74, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output, WithMarkdownTables(true)); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "| --- |") {
		t.Fatalf("paragraphs wrongly detected as table: %q", got)
	}
}

func TestMarkdownTablesDefaultOff(t *testing.T) {
	// 表格识别默认关闭，双栏正文/公式排版不会被误判。
	doc := tablePage(
		textItemAt("序号", true, 30, 52, 5.2),
		textItemAt("编号", true, 49, 52, 5.2),
		textItemAt("158", true, 30, 63, 5.2),
		textItemAt("桒-14", true, 47, 63, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "| --- |") {
		t.Fatalf("table detection should be off by default: %q", got)
	}
	if !strings.Contains(got, "序号") || !strings.Contains(got, "158") {
		t.Fatalf("content lost: %q", got)
	}
}

func TestMarkdownTableEscapesPipe(t *testing.T) {
	// 单元格中的竖线必须转义，避免破坏 GFM 表格结构。
	doc := tablePage(
		textItemAt("列A", true, 30, 52, 5.2),
		textItemAt("列B", true, 90, 52, 5.2),
		textItemAt("a|b", true, 30, 63, 5.2),
		textItemAt("c", true, 90, 63, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output, WithMarkdownTables(true)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `a\|b`) {
		t.Fatalf("pipe not escaped: %q", output.String())
	}
}
