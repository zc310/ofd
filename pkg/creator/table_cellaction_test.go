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
