package creator

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// runsTable 用预解析片段造一张表。
func runsTable(rows [][]TextRun, headerRows int, align []string) Table {
	build := func(list [][]TextRun) []TableRow {
		out := make([]TableRow, 0, len(list))
		for _, list := range list {
			cells := make([]TableCell, 0, len(list))
			for index, run := range list {
				cell := TableCell{Runs: []TextRun{run}, Size: 4.2}
				if index < len(align) {
					cell.Align = align[index]
				}
				cells = append(cells, cell)
			}
			out = append(out, TableRow{Cells: cells})
		}
		return out
	}
	return Table{
		X: 20, Y: 30, Width: 160, HeaderRows: headerRows, Rows: build(rows),
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
}

// 预解析片段按片段渲染样式，不必先拼成 markdown 源码。
func TestTableCellRunsCarryStyles(t *testing.T) {
	table := runsTable([][]TextRun{
		{{Text: "粗", Bold: true}, {Text: "体"}},
		{{Text: "普通"}, {Text: "代码", Code: true}},
		{{Text: "删除", Strike: true}, {Text: "斜体", Italic: true}},
	}, 1, nil)
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	texts, strikes, _ := layout.generateCellTexts(layout.segments[0])
	var got []string
	for _, text := range texts {
		switch {
		case text.Weight == 700:
			got = append(got, "bold:"+text.Value)
		case text.Italic:
			got = append(got, "italic:"+text.Value)
		default:
			got = append(got, "plain:"+text.Value)
		}
	}
	want := []string{"bold:粗", "plain:体", "plain:普通", "plain:代码", "plain:删除", "italic:斜体"}
	if len(got) != len(want) {
		t.Fatalf("文字对象 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 段 = %q, want %q", i+1, got[i], want[i])
		}
	}
	if len(strikes) != 1 {
		t.Errorf("删除线应产出 1 条，实际 %d 条", len(strikes))
	}
}

// 片段带 Href 时只有该段可点并按链接蓝着色，cell.Actions 兜住其余段。
func TestTableCellRunsHrefPerSegment(t *testing.T) {
	table := runsTable([][]TextRun{
		{{Text: "表头"}},
		{{Text: "普通"}, {Text: "内链", Href: "https://run.example"}},
	}, 1, []string{"left"})
	// 单元格动作挂在「普通」那一格：它没有片段级链接，应沿用整格地址。
	table.Rows[1].Cells[0].Actions = []Action{{
		Event: ActionEventClick, URI: &URIAction{URI: "https://cell.example"},
	}}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	texts, _, _ := layout.generateCellTexts(layout.segments[0])
	for _, text := range texts {
		switch text.Value {
		case "内链":
			if !sameRGB(text.FillColor, defaultTableLinkColor) {
				t.Errorf("内链段应着色，实际 %+v", text.FillColor)
			}
			if len(text.Actions) != 1 || text.Actions[0].URI == nil ||
				text.Actions[0].URI.URI != "https://run.example" {
				t.Errorf("内链段的动作 = %+v, want run.example", text.Actions)
			}
		case "普通":
			if text.FillColor != nil {
				t.Errorf("普通段不应着色，实际 %+v", *text.FillColor)
			}
			if len(text.Actions) != 1 || text.Actions[0].URI.URI != "https://cell.example" {
				t.Errorf("普通段应沿用单元格动作，实际 %+v", text.Actions)
			}
		}
	}
}

// Runs 与 Value/Format 同时设置要报错：调用方只会以为是自己写的那套生效了。
// 与 manifest 的「文件路径与 data_base64 互斥」同理。
func TestTableCellRunsExclusiveWithValue(t *testing.T) {
	both := Table{
		X: 20, Y: 30, Width: 120,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲", Runs: []TextRun{{Text: "甲"}}}}}},
	}
	if err := validateTable(both, 1, nil, nil, nil, nil); err == nil {
		t.Error("同时设置 Runs 与 Value 应报错")
	}
	format := Table{
		X: 20, Y: 30, Width: 120,
		Rows: []TableRow{{Cells: []TableCell{{
			Value: "**甲**", Format: "markdown", Runs: []TextRun{{Text: "甲"}},
		}}}},
	}
	if err := validateTable(format, 1, nil, nil, nil, nil); err == nil {
		t.Error("同时设置 Runs 与 Format 应报错")
	}
	empty := Table{
		X: 20, Y: 30, Width: 120,
		Rows: []TableRow{{Cells: []TableCell{{Runs: []TextRun{{Text: "甲"}, {Text: ""}}}}}},
	}
	if err := validateTable(empty, 1, nil, nil, nil, nil); err == nil {
		t.Error("空文字片段应报错")
	}
}

// 端到端：片段的样式与链接都要落到 OFD 的文字对象上。
func TestTableCellRunsSurviveToOFD(t *testing.T) {
	var output bytes.Buffer
	if err := CreateWithOptions(Document{
		ID: "runs", PageSize: A4,
		Pages: []Page{{Items: []Item{runsTable([][]TextRun{
			{{Text: "名称"}, {Text: "链接"}},
			{{Text: "粗", Bold: true}, {Text: "站点", Href: "https://example.com/runs"}},
		}, 1, nil)}}},
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
	var bold, linked bool
	for _, item := range page.Content().Layer[0].Items {
		if item.Kind != models.PageItemText || item.Text == nil {
			continue
		}
		if item.Text.Weight == 700 && item.Text.TextCode[0].Value == "粗" {
			bold = true
		}
		if item.Text.Actions != nil {
			for _, action := range item.Text.Actions.Action {
				if action.URI != nil && action.URI.URI == "https://example.com/runs" {
					linked = true
				}
			}
		}
	}
	if !bold {
		t.Error("产物里没有加粗的片段")
	}
	if !linked {
		t.Error("产物里没有带链接的片段")
	}
}
