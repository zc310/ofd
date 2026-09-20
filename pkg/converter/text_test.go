package converter

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestTextDocumentExtractsPageTextInOrder(t *testing.T) {
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItem("1", true),
				textItem("2", true),
				{Kind: models.PageItemBlock, Block: models.PageBlock{CTPageBlock: models.CTPageBlock{
					Items: []models.PageItem{textItem("3", true)},
				}}},
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	// 默认模式按行合并：同一 Y 的三个对象在一行，按 X 排序
	if got, want := output.String(), "1 2 3\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestTextDocumentSeparatesPagesAndSupportsPageSelection(t *testing.T) {
	doc := &parser.Document{Pages: []*parser.Page{
		parser.NewPage(models.PageContent{Content: textContent("p1")}),
		parser.NewPage(models.PageContent{Content: textContent("p2")}),
	}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "p1\n\np2\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}

	output.Reset()
	if err := TextDocument(doc, &output, Page(2)); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "p2\n"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestTextDocumentsRejectInvalidGlobalPage(t *testing.T) {
	documents := []*parser.Document{{Pages: []*parser.Page{parser.NewPage(models.PageContent{})}}}
	for _, page := range []int{-1, 2} {
		var output bytes.Buffer
		err := TextDocuments(documents, &output, Page(page))
		if err == nil {
			t.Fatalf("Page(%d) returned nil error", page)
		}
		if !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("Page(%d) error = %v, want ErrInvalidPage", page, err)
		}
	}
}

func TestTextDocumentsUseGlobalPageNumbers(t *testing.T) {
	documents := []*parser.Document{
		{Pages: []*parser.Page{parser.NewPage(models.PageContent{Content: textContent("d1p1")})}},
		{Pages: []*parser.Page{
			parser.NewPage(models.PageContent{Content: textContent("d2p1")}),
			parser.NewPage(models.PageContent{Content: textContent("d2p2")}),
		}},
	}

	var output bytes.Buffer
	if err := TextDocuments(documents, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "d1p1\n\nd2p1\n\nd2p2\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}

	output.Reset()
	if err := TextDocuments(documents, &output, Page(2)); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "d2p1\n"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestTextDocumentSkipsInvisibleText(t *testing.T) {
	content := &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
		Items: []models.PageItem{
			textItem("vis", true),
			textItem("invis", false),
		},
	}}}}

	var output bytes.Buffer
	if err := TextDocument(&parser.Document{Pages: []*parser.Page{parser.NewPage(models.PageContent{Content: content})}}, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "vis\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestMarkdownDocumentFormatsPagesAndEscapesMarkdown(t *testing.T) {
	doc := &parser.Document{Pages: []*parser.Page{
		parser.NewPage(models.PageContent{Content: textContent("# title")}),
		parser.NewPage(models.PageContent{Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("*emph* [link]", true, 10, 10, 12),
				textItemAt("1. list", true, 10, 20, 12),
			},
		}}}}})}}

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	// 新格式: 无页面标题，编号标题自动识别为 H3，章节标题识别为 H2
	want := "# OFD 文档\n\n\\# title\n\n---\n\n\\*emph\\* \\[link\\]\n\n### 1\\. list\n"
	if got := output.String(); got != want {
		t.Fatalf("markdown = %q, want %q", got, want)
	}

	output.Reset()
	if err := MarkdownDocument(doc, &output, Page(2)); err != nil {
		t.Fatal(err)
	}
	// Page(2) 只处理第2页，但输出时页面索引是基于过滤后的结果（即第1页）
	if want := "# OFD 文档\n\n\\*emph\\* \\[link\\]\n\n### 1\\. list\n"; output.String() != want {
		t.Fatalf("selected markdown = %q, want %q", output.String(), want)
	}
}

func textItem(value string, visible bool) models.PageItem {
	object := models.TextObject{CtText: models.CtText{TextCode: []models.TextCode{{Value: value}}}}
	object.Visible.Set(visible)
	return models.PageItem{Kind: models.PageItemText, Text: object}
}

func textItemAt(value string, visible bool, x, y, size float64) models.PageItem {
	object := models.TextObject{
		CtText: models.CtText{
			TextCode: []models.TextCode{{Value: value, X: x, Y: y}},
			Size:     size,
		},
	}
	object.Visible.Set(visible)
	return models.PageItem{Kind: models.PageItemText, Text: object}
}

func textContent(value string) *models.Content {
	return &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{Items: []models.PageItem{textItem(value, true)}}}}}
}

func TestTextDocumentLayoutModeIndentByPosition(t *testing.T) {
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("L", true, 10, 10, 4),
				textItemAt("R", true, 60, 10, 4),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	t.Logf("layout output: %q", got)
	if !strings.Contains(got, "L") || !strings.Contains(got, "R") {
		t.Fatalf("both texts present: %q", got)
	}
	if strings.Index(got, "R") <= strings.Index(got, "L") {
		t.Fatalf("right should appear after left: %q", got)
	}
}

func TestTextDocumentLayoutModeSeparatesRowsByY(t *testing.T) {
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("Row1", true, 10, 10, 4),
				textItemAt("Row2", true, 10, 20, 4),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	t.Logf("layout output: %q", got)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "Row1") || !strings.Contains(lines[1], "Row2") {
		t.Fatalf("lines order: %q", got)
	}
}

func TestTextDocumentLayoutModeDefaultBehavior(t *testing.T) {
	// 布局模式现在是默认行为：同一行的文字按 X 排序并用空格对齐到列位置
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("A", true, 60, 10, 4),
				textItemAt("B", true, 10, 10, 4),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	// B 在 x=10，A 在 x=60，同一行 → 布局模式会按列位置补空格对齐
	t.Logf("default layout output: %q", got)
	if !strings.Contains(got, "B") || !strings.Contains(got, "A") {
		t.Fatalf("both texts present: %q", got)
	}
	// B 应该在 A 前面
	if strings.Index(got, "B") >= strings.Index(got, "A") {
		t.Fatalf("B should appear before A: %q", got)
	}
}

func TestTextDocumentLayoutCountsWideCharactersAsTwoColumns(t *testing.T) {
	// 全角字符按两列宽度参与列对齐，避免后继文字与 CJK 字符重叠。
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("中", true, 10, 10, 4),
				textItemAt("R", true, 60, 10, 4),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	// 列单位 210/80 = 2.625，R 的列号 round(50/2.625)=19，中占两列 → 17 个空格
	want := "中" + strings.Repeat(" ", 17) + "R\n"
	if got := output.String(); got != want {
		t.Fatalf("wide char alignment = %q, want %q", got, want)
	}
}

func TestTextDocumentUsesPagePhysicalWidthForAlignment(t *testing.T) {
	// 使用页面实际物理宽度排版：窄页面单位更小，同一 X 偏移得到更大的列号。
	page := parser.NewPage(models.PageContent{
		Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 105, Height: 148}},
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("L", true, 10, 10, 4),
				textItemAt("R", true, 60, 10, 4),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	// 列单位 105/80 = 1.3125，R 的列号 round(50/1.3125)=38，L 占一列 → 37 个空格
	want := "L" + strings.Repeat(" ", 37) + "R\n"
	if got := output.String(); got != want {
		t.Fatalf("page width alignment = %q, want %q", got, want)
	}
}

func TestMarkdownDocumentsSkipsLeadingSeparatorForEmptyFirstPage(t *testing.T) {
	// 首个页面没有文字时，不应在正文前输出孤立的分页分隔线。
	doc := &parser.Document{Pages: []*parser.Page{
		parser.NewPage(models.PageContent{Content: &models.Content{}}),
		parser.NewPage(models.PageContent{Content: textContent("正文")}),
	}}

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "---\n\n正文") {
		t.Fatalf("unexpected leading separator: %q", got)
	}
	if !strings.Contains(got, "正文") {
		t.Fatalf("content missing: %q", got)
	}
}

func TestMarkdownDocumentsClampsHeadingLevelToSix(t *testing.T) {
	// 页码标题层级加 2 后不能超过 Markdown 允许的六级标题。
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt("Big", true, 10, 10, 12),
				textItemAt("Small", true, 10, 30, 6),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "#######") {
		t.Fatalf("heading deeper than six levels: %q", got)
	}
	if !strings.Contains(got, "###### Small") {
		t.Fatalf("expected clamped level-6 heading: %q", got)
	}
}

func TestMarkdownUsesDocumentTitle(t *testing.T) {
	page := parser.NewPage(models.PageContent{Content: textContent("正文")})
	doc := &render.Document{Document: &parser.Document{Pages: []*parser.Page{page}}}
	var output bytes.Buffer
	conv := newConverter()
	conv.docTitle = "年度报告"
	if err := markdownDocuments([]*render.Document{doc}, &output, conv); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "# 年度报告\n\n") {
		t.Fatalf("markdown title = %q", output.String())
	}
}

func TestMarkdownTitleFallsBackToDefault(t *testing.T) {
	page := parser.NewPage(models.PageContent{Content: textContent("正文")})
	doc := &render.Document{Document: &parser.Document{Pages: []*parser.Page{page}}}
	var output bytes.Buffer
	if err := markdownDocuments([]*render.Document{doc}, &output, newConverter()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "# OFD 文档\n\n") {
		t.Fatalf("markdown title = %q", output.String())
	}
}

func TestMarkdownAddsBlankLineBeforeIndentedParagraph(t *testing.T) {
	// 段首缩进（行左边缘明显大于正文左边距）应触发段落空行，续行不触发。
	doc := tablePage(
		textItemAt("甲段第一行", true, 30, 52, 5.2),
		textItemAt("甲段续行", true, 30, 60, 5.2),
		textItemAt("乙段第一行", true, 40, 68, 5.2),
		textItemAt("乙段续行", true, 30, 76, 5.2),
		textItemAt("丙段第一行", true, 30, 84, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "甲段续行\n\n乙段第一行") {
		t.Fatalf("indented paragraph not separated: %q", got)
	}
	if !strings.Contains(got, "乙段第一行\n乙段续行") {
		t.Fatalf("continuation line wrongly separated: %q", got)
	}
}

func TestMarkdownAddsBlankLineBeforeArticleMarker(t *testing.T) {
	// “第 X 条”等段落标记应触发段落空行，即使没有缩进。
	doc := tablePage(
		textItemAt("前言部分，说明本规定的目的和依据。", true, 30, 52, 5.2),
		textItemAt("第一条 为了规范相关活动，保障各方合法权益，促进健康发展。", true, 30, 60, 5.2),
		textItemAt("第二条 本规定适用于相关领域的管理活动。", true, 30, 68, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "目的和依据。\n\n第一条") {
		t.Fatalf("article marker not separated: %q", got)
	}
	if !strings.Contains(got, "健康发展。\n\n第二条") {
		t.Fatalf("second article not separated: %q", got)
	}
}

func TestMarkdownKeepsMidPageNumberRow(t *testing.T) {
	// 页面中部的纯数字行是数据（如代码行号），不能当页码删除。
	doc := tablePage(
		textItemAt("正文第一段", true, 30, 50, 5.2),
		textItemAt("2", true, 30, 100, 5.2),
		textItemAt("正文第二段", true, 30, 150, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\n2\n") {
		t.Fatalf("mid-page number wrongly dropped: %q", output.String())
	}
}

func TestMarkdownDropsPageEdgeNumberRows(t *testing.T) {
	// 页面顶部/底部的纯数字和破折号页码仍应过滤。
	doc := tablePage(
		textItemAt("2", true, 30, 5, 5.2),
		textItemAt("正文内容", true, 30, 100, 5.2),
		textItemAt("12", true, 30, 290, 5.2),
		textItemAt("— 4 —", true, 30, 293, 5.2),
	)

	var output bytes.Buffer
	if err := MarkdownDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "\n2\n") || strings.Contains(got, "\n12\n") || strings.Contains(got, "4") {
		t.Fatalf("page-edge page numbers not filtered: %q", got)
	}
	if !strings.Contains(got, "正文内容") {
		t.Fatalf("body content lost: %q", got)
	}
}

func TestMarkdownOutputIsDeterministic(t *testing.T) {
	// 两组正文左边距数量相同时，段落判决不能受 map 迭代顺序影响。
	doc := tablePage(
		textItemAt("甲组第一行内容", true, 30, 50, 5.2),
		textItemAt("甲组第二行内容", true, 30, 60, 5.2),
		textItemAt("乙组第一行内容", true, 40, 70, 5.2),
		textItemAt("乙组第二行内容", true, 40, 80, 5.2),
	)

	var first string
	for i := 0; i < 20; i++ {
		var output bytes.Buffer
		if err := MarkdownDocument(doc, &output); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = output.String()
			continue
		}
		if output.String() != first {
			t.Fatalf("markdown output differs between runs:\n first=%q\n now  =%q", first, output.String())
		}
	}
}

func TestTextDocumentSanitizesNonUTF8TextValue(t *testing.T) {
	// OFD 文本若不是 UTF-8（如第三方写入 GBK 字节），转 txt 时也应输出中文。
	page := parser.NewPage(models.PageContent{
		Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{
			Items: []models.PageItem{
				textItemAt(string([]byte{0xD6, 0xD0, 0xCE, 0xC4}), true, 10, 10, 4),
			},
		}}}}})
	doc := &parser.Document{Pages: []*parser.Page{page}}

	var output bytes.Buffer
	if err := TextDocument(doc, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "中文\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}
