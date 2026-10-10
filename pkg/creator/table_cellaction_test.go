package creator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// cellActionTable 造一张两行两列的表，第二行第二格带一个外链动作。
func cellActionTable() Table {
	uri := Action{Event: ActionEventClick, URI: &URIAction{URI: "https://example.com/cell"}}
	return Table{
		X: 20, Y: 30, Width: 160, HeaderRows: 1,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "名称", Size: 4.2}, {Value: "说明", Size: 4.2}}},
			{Cells: []TableCell{
				{Value: "示例", Size: 4.2},
				{Value: "整格可点", Size: 4.2, Actions: []Action{uri}},
			}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
}

// 单元格动作要落到该格拆出的每一个文字对象上。
//
// 单元格文字按样式拆成多个文字对象，漏掉任何一个，那一段就没热区——点击时
// 表现为「格子里有的地方能点、有的地方不能」。这是与文字对象级动作的关键差别。
func TestTableCellActionCoversEveryTextRun(t *testing.T) {
	uri := Action{Event: ActionEventClick, URI: &URIAction{URI: "https://example.com/cell"}}
	// 用 Markdown 格式让同一格拆成多个样式的文字对象。
	table := Table{
		X: 20, Y: 30, Width: 160, HeaderRows: 1,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "名称", Size: 4.2}}},
			{Cells: []TableCell{{Value: "**粗体**与`代码`混排", Size: 4.2,
				Format: "markdown", Actions: []Action{uri}}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	texts, _, _ := layout.generateCellTexts(layout.segments[0])
	if len(texts) < 2 {
		t.Fatalf("混排单元格应拆成多个文字对象，实际 %d 个", len(texts))
	}
	for index, text := range texts {
		if text.Value == "名称" {
			continue
		}
		if len(text.Actions) != 1 || text.Actions[0].URI == nil ||
			text.Actions[0].URI.URI != "https://example.com/cell" {
			t.Errorf("第 %d 个文字对象 %q 没有带上单元格动作，实际 %+v", index+1, text.Value, text.Actions)
		}
	}
}

// 没有动作的单元格不受影响：表头行不能因为下面某格有链接就整行可点。
func TestTableCellWithoutActionHasNone(t *testing.T) {
	layout, err := layoutTable(cellActionTable(), 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	texts, _, _ := layout.generateCellTexts(layout.segments[0])
	for _, text := range texts {
		switch text.Value {
		case "名称", "说明", "示例":
			if len(text.Actions) != 0 {
				t.Errorf("未设动作的单元格 %q 不应带动作，实际 %+v", text.Value, text.Actions)
			}
		}
	}
}

// 单元格动作与其他图元动作一样要过校验：空 URI、目标页越界都得在生成阶段拦下，
// 否则错误会推迟到产物里才暴露。
func TestTableCellActionValidated(t *testing.T) {
	emptyURI := Table{
		X: 20, Y: 30, Width: 120,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲", Actions: []Action{{URI: &URIAction{}}}}}}},
	}
	if err := validateTable(emptyURI, 1, nil, nil, nil, nil); err == nil {
		t.Error("空 URI 的单元格动作应报错")
	}

	both := Table{
		X: 20, Y: 30, Width: 120,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲", Actions: []Action{{
			URI:  &URIAction{URI: "https://example.com"},
			Goto: &GotoAction{Page: 1},
		}}}}}},
	}
	if err := validateTable(both, 1, nil, nil, nil, nil); err == nil {
		t.Error("同时设置 URI 与 Goto 的单元格动作应报错")
	}

	outOfRange := Table{
		X: 20, Y: 30, Width: 120,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲", Actions: []Action{{
			Goto: &GotoAction{Page: 9},
		}}}}}},
	}
	if err := validateTable(outOfRange, 2, nil, nil, nil, nil); err == nil {
		t.Error("目标页越界的单元格动作应报错")
	}
}

// 端到端：动作要真的写进 OFD 并能被 PDF 导出取到。
func TestTableCellActionSurvivesToOFD(t *testing.T) {
	var output bytes.Buffer
	if err := CreateWithOptions(Document{
		ID: "cellaction", PageSize: A4,
		Pages: []Page{{Items: []Item{cellActionTable()}}},
	}, &output, CreateOptions{}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatalf("解析产物失败: %v", err)
	}
	defer ofd.Close()

	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	content := page.Content()
	if content == nil || len(content.Layer) == 0 {
		t.Fatalf("页面没有图层: content=%v", content)
	}
	if content.Layer[0] == nil {
		t.Fatalf("首个图层为 nil，图层数 %d", len(content.Layer))
	}
	linked := 0
	for _, item := range content.Layer[0].Items {
		if item.Kind != models.PageItemText || item.Text == nil {
			continue
		}
		if item.Text.Actions == nil {
			continue
		}
		for _, action := range item.Text.Actions.Action {
			if action.URI != nil && action.URI.URI == "https://example.com/cell" {
				linked++
			}
		}
	}
	if linked == 0 {
		t.Error("产物里没有带 URI 动作的单元格文字")
	}
	if linked > 2 {
		t.Errorf("只有第二个单元格设了动作，实际 %d 个文字对象带动作", linked)
	}
}

// 跨页表格的续页片段也要带上动作：拆页后片段由 tableFragments 重建，单元格内容
// 仍走同一条落笔路径。
func TestTableCellActionKeptOnSplitFragment(t *testing.T) {
	uri := Action{Event: ActionEventClick, URI: &URIAction{URI: "https://example.com/cell"}}
	rows := []TableRow{{Cells: []TableCell{{Value: "表头", Size: 4.2}}}}
	for i := 0; i < 80; i++ {
		rows = append(rows, TableRow{Cells: []TableCell{
			{Value: "行", Size: 4.2},
			{Value: strings.Repeat("内", 3), Size: 4.2, Actions: []Action{uri}},
		}})
	}
	table := Table{
		X: 20, Y: 30, Width: 120, HeaderRows: 1, Rows: rows,
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
	var output bytes.Buffer
	if err := CreateWithOptions(Document{
		ID: "cellsplit", PageSize: A4, Pages: []Page{{Items: []Item{table}}},
	}, &output, CreateOptions{}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if len(ofd.Documents[0].Pages) < 2 {
		t.Skip("表格未跨页，本用例不适用")
	}
	linked := 0
	for _, page := range ofd.Documents[0].Pages {
		if err := page.EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
		for _, layer := range page.Content().Layer {
			for _, item := range layer.Items {
				if item.Kind != models.PageItemText || item.Text == nil {
					continue
				}
				if item.Text.Actions == nil {
					continue
				}
				for _, action := range item.Text.Actions.Action {
					if action.URI != nil && action.URI.URI == "https://example.com/cell" {
						linked++
					}
				}
			}
		}
	}
	if linked == 0 {
		t.Error("跨页后仍有单元格的链接丢失")
	}
}

// markdownCellRun 跑一张单行表并返回该格拆出的文字对象。
func markdownCellRun(t *testing.T, value string, cellActions []Action) []Text {
	t.Helper()
	table := Table{
		X: 20, Y: 30, Width: 170,
		Rows:  []TableRow{{Cells: []TableCell{{Value: value, Size: 4.2, Format: "markdown", Actions: cellActions}}}},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	texts, _, _ := layout.generateCellTexts(layout.segments[0])
	return texts
}

// sameRGB 比较两个颜色的 RGB 分量，忽略颜色空间分量。
func sameRGB(a, b *Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.R == b.R && a.G == b.G && a.B == b.B
}

// textURI 返回文字对象上的 URI，没有则返回空串。
func textURI(text Text) string {
	if len(text.Actions) == 1 && text.Actions[0].URI != nil {
		return text.Actions[0].URI.URI
	}
	return ""
}

// Markdown 链接只让被包住的那一段可点，同格的普通文字不带动作。
//
// 这是与 TableCell.Actions 的分工：单元格动作作用于整格，段内链接更具体，优先。
func TestMarkdownCellLinkIsPerRun(t *testing.T) {
	texts := markdownCellRun(t, "见 [项目主页](https://github.com/zc310/ofd) 说明", nil)
	if len(texts) < 2 {
		t.Fatalf("应有普通文字与链接文字两段，实际 %d 段", len(texts))
	}
	linked := 0
	for _, text := range texts {
		if textURI(text) == "https://github.com/zc310/ofd" {
			linked++
			if text.Value != "项目主页" {
				t.Errorf("可点的应是链接文字本身，实际 %q", text.Value)
			}
			continue
		}
		if len(text.Actions) != 0 {
			t.Errorf("同格的普通文字 %q 不应带动作，实际 %+v", text.Value, text.Actions)
		}
	}
	if linked != 1 {
		t.Errorf("链接文字应恰好 1 段带动作，实际 %d 段", linked)
	}
}

// 自动链接（<https://example.net>）的显示文字就是网址，同样可点。
func TestMarkdownCellAutoLinkIsClickable(t *testing.T) {
	texts := markdownCellRun(t, "见 <https://example.net> 说明", nil)
	found := false
	for _, text := range texts {
		if textURI(text) == "https://example.net" {
			found = true
		}
	}
	if !found {
		t.Errorf("自动链接应可点，实际 %+v", texts)
	}
}

// 纯锚点与空目标不生成动作：表格单元格没有大纲可跳，标题也不会被写成锚点。
//
// 文字仍要完整显示——丢字比不可点更糟。
func TestMarkdownCellAnchorAndEmptyLinkHaveNoAction(t *testing.T) {
	texts := markdownCellRun(t, "锚点 [跳](#第一节) 与 [空]()", nil)
	// 文字对象的首尾空格按惯例裁掉、改由 X 偏移补回，所以只能逐段核对内容，
	// 不能直接拼接——拼接出来的是 "锚点跳与空"，渲染上却有空格。
	var seen []string
	for _, text := range texts {
		seen = append(seen, text.Value)
		if len(text.Actions) != 0 {
			t.Errorf("锚点或空目标不应生成动作，%q 带了 %+v", text.Value, text.Actions)
		}
	}
	want := map[string]bool{"锚点": true, "跳": true, "与": true, "空": true}
	if len(seen) != len(want) {
		t.Fatalf("文字应完整保留为 %v，实际 %v", want, seen)
	}
	for _, value := range seen {
		if !want[value] {
			t.Errorf("出现了意外的文字段 %q，合并结果 %v", value, seen)
		}
	}
}

// 相邻的两个不同链接不能并成一段：样式全同，并了就只挂前一个地址。
func TestMarkdownCellAdjacentLinksStaySeparate(t *testing.T) {
	texts := markdownCellRun(t, "[甲](https://a.example) 与 [乙](https://b.example)", nil)
	got := map[string]string{}
	for _, text := range texts {
		if uri := textURI(text); uri != "" {
			got[text.Value] = uri
		}
	}
	if got["甲"] != "https://a.example" {
		t.Errorf("第一个链接丢失或地址不对: %q -> %q", "甲", got["甲"])
	}
	if got["乙"] != "https://b.example" {
		t.Errorf("第二个链接被并进第一个: %q -> %q", "乙", got["乙"])
	}
}

// 单元格动作与段内链接同时存在时，段内链接优先，格子其余文字沿用单元格动作。
func TestMarkdownCellRunLinkWinsOverCellAction(t *testing.T) {
	cell := []Action{{Event: ActionEventClick, URI: &URIAction{URI: "https://cell.example"}}}
	texts := markdownCellRun(t, "普通 [内链](https://run.example)", cell)
	sawCell, sawRun := false, false
	for _, text := range texts {
		switch textURI(text) {
		case "https://run.example":
			sawRun = true
			if text.Value != "内链" {
				t.Errorf("段内链接应只覆盖内链那段，实际 %q", text.Value)
			}
		case "https://cell.example":
			sawCell = true
			if strings.Contains(text.Value, "内链") {
				t.Errorf("内链那段不应再挂单元格地址，实际 %q", text.Value)
			}
		default:
			if len(text.Actions) != 0 {
				t.Errorf("未预期的动作 %q: %+v", text.Value, text.Actions)
			}
		}
	}
	if !sawRun || !sawCell {
		t.Errorf("段内链接与单元格地址都应出现，实际 run=%v cell=%v", sawRun, sawCell)
	}
}

// 链接文字按链接蓝着色，表格其余文字保持默认色。
//
// 颜色与正文链接一致（internal/layout 的 colorLink），同一份文档里正文与表格不该
// 是两种蓝。链接着色与能否点击是两回事：纯锚点不着动作但仍着色，与正文一致。
func TestMarkdownCellLinkIsColoured(t *testing.T) {
	texts := markdownCellRun(t, "普通 [链接](https://example.com) 普通", nil)
	for _, text := range texts {
		isLink := text.Value == "链接"
		if isLink {
			if !sameRGB(text.FillColor, defaultTableLinkColor) {
				t.Errorf("链接文字颜色 = %+v, want %+v", text.FillColor, defaultTableLinkColor)
			}
			if text.Fill == nil || !*text.Fill {
				t.Error("链接文字应显式填充，否则颜色不生效")
			}
			continue
		}
		if text.FillColor != nil {
			t.Errorf("非链接文字 %q 不应着色，实际 %+v", text.Value, *text.FillColor)
		}
	}
}

// 纯锚点按链接蓝着色但不生成动作。
func TestMarkdownCellAnchorColouredButNotClickable(t *testing.T) {
	texts := markdownCellRun(t, "[跳](#第一节)", nil)
	if len(texts) != 1 {
		t.Fatalf("应只有一段文字，实际 %d 段", len(texts))
	}
	if len(texts[0].Actions) != 0 {
		t.Errorf("纯锚点不应生成动作，实际 %+v", texts[0].Actions)
	}
	if !sameRGB(texts[0].FillColor, defaultTableLinkColor) {
		t.Errorf("纯锚点仍应按链接着色，实际 %+v", texts[0].FillColor)
	}
}

// TableStyle.LinkColor 可覆盖默认链接色，与 StrikeColor、RuleColor 同一套做法。
func TestMarkdownCellLinkColorOverridable(t *testing.T) {
	want := Color{R: 1, G: 2, B: 3}
	table := Table{
		X: 20, Y: 30, Width: 170,
		Rows:  []TableRow{{Cells: []TableCell{{Value: "[链接](https://example.com)", Size: 4.2, Format: "markdown"}}}},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true, LinkColor: &want},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	texts, _, _ := layout.generateCellTexts(layout.segments[0])
	if len(texts) != 1 || !sameRGB(texts[0].FillColor, &want) {
		t.Fatalf("链接色未按 TableStyle.LinkColor 覆盖: %+v", texts)
	}
}
