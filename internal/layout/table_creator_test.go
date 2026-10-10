package layout

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// expandDocument 把排版结果真正交给创建器展开，返回解析后的 OFD 页面。
//
// 表格改用 creator.Table 之后，边框线、表头底色与单元格文字都由创建器在展开阶段
// 生成，layout 产出里只剩一个 Table 图元。断言必须落在展开后的结果上——对着
// layout 的中间产物断言，等于在测一个早就换掉的实现。
func expandDocument(t *testing.T, document *creator.Document, options Options) []*parser.Page {
	t.Helper()
	var output bytes.Buffer
	if err := creator.CreateWithOptions(*document, &output, creator.CreateOptions{}); err != nil {
		t.Fatalf("创建 OFD 失败: %v", err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatalf("解析产物失败: %v", err)
	}
	t.Cleanup(func() { ofd.Close() })
	pages := ofd.Documents[0].Pages
	for _, page := range pages {
		if err := page.EnsureLoaded(); err != nil {
			t.Fatalf("加载页面失败: %v", err)
		}
	}
	return pages
}

// pageItems 收集页面各图层的全部页面对象。
func pageItems(page *parser.Page) []models.PageItem {
	var items []models.PageItem
	for _, layer := range page.Content().Layer {
		if layer == nil {
			continue
		}
		items = append(items, layer.Items...)
	}
	return items
}

// pageTextValues 返回页面上所有文字的可见内容。
func pageTextValues(page *parser.Page) []string {
	var values []string
	for _, item := range pageItems(page) {
		if item.Kind != models.PageItemText || item.Text == nil {
			continue
		}
		for _, code := range item.Text.TextCode {
			values = append(values, code.Value)
		}
	}
	return values
}

// tableOfCells 造一张最小表格块。
func tableOfCells(header []Cell, rows [][]Cell) Block {
	return Block{Kind: KindTable, Table: &Table{Header: header, Rows: rows}}
}

// 表格的表头底色由 creator 展开时生成，layout 只提供一个 Table 图元。
func TestBuildTableHeaderFillFromCreator(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{tableOfCells(
		[]Cell{{{Text: "名称"}}, {{Text: "数量"}}},
		[][]Cell{{{{Text: "苹果"}}, {{Text: "3"}}}},
	)}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	pages := expandDocument(t, document, DefaultOptions())
	found := false
	for _, item := range pageItems(pages[0]) {
		if item.Kind == models.PageItemPath && item.Path != nil &&
			item.Path.FillColor != nil && item.Path.FillColor.Value != nil {
			if values, count, ok := item.Path.FillColor.Value.Components(); ok && count >= 3 &&
				values[0] == 239 && values[1] == 242 && values[2] == 245 {
				found = true
			}
		}
	}
	if !found {
		t.Error("未找到表头背景，creator 没有把 HeaderFillColor 落成填充路径")
	}
}

// 表头文字必须落在表头底色带内，不能压到数据行上。
func TestBuildTableHeaderTextInsideFill(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{tableOfCells(
		[]Cell{{{Text: "名称"}}, {{Text: "数量"}}},
		[][]Cell{{{{Text: "苹果"}}, {{Text: "3"}}}},
	)}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	pages := expandDocument(t, document, DefaultOptions())
	var band *models.StBox
	for _, item := range pageItems(pages[0]) {
		if item.Kind == models.PageItemPath && item.Path != nil &&
			item.Path.FillColor != nil && item.Path.FillColor.Value != nil {
			if values, count, ok := item.Path.FillColor.Value.Components(); ok && count >= 3 &&
				values[0] == 239 && values[1] == 242 && values[2] == 245 {
				box := item.Path.Boundary
				band = &box
			}
		}
	}
	if band == nil {
		t.Fatal("未找到表头背景")
	}
	for _, value := range pageTextValues(pages[0]) {
		if value != "名称" && value != "数量" {
			continue
		}
		for _, item := range pageItems(pages[0]) {
			if item.Kind != models.PageItemText || item.Text == nil {
				continue
			}
			for _, code := range item.Text.TextCode {
				if code.Value != value {
					continue
				}
				box := item.Text.Boundary
				if box.Y < band.Y-0.5 || box.Y+box.Height > band.Y+band.Height+0.5 {
					t.Errorf("表头文字 %q 的纵向范围 %.2f..%.2f 越出底色带 %.2f..%.2f",
						value, box.Y, box.Y+box.Height, band.Y, band.Y+band.Height)
				}
			}
		}
	}
}

// 表格单元格里的链接要落到展开后的文字对象上。
//
// 表格改用 creator.Table 之后，layout 不再直接产出文字对象，链接由 TextRun.Href
// 经 creator 展开时生成动作。
func TestBuildTableCellLinkReachesOFD(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{tableOfCells(
		[]Cell{{{Text: "名称"}}, {{Text: "链接"}}},
		[][]Cell{{{{Text: "示例"}}, {{Text: "站点", Link: true, Href: "https://example.com/cell"}}}},
	)}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	pages := expandDocument(t, document, DefaultOptions())
	found := false
	for _, item := range pageItems(pages[0]) {
		if item.Kind != models.PageItemText || item.Text == nil || item.Text.Actions == nil {
			continue
		}
		for _, action := range item.Text.Actions.Action {
			if action.URI != nil && action.URI.URI == "https://example.com/cell" {
				found = true
			}
		}
	}
	if !found {
		t.Error("表格单元格里的链接没有落成文字对象的动作")
	}
}

// 表格跨页时续页要重复表头行。
//
// 这是改用 creator.Table 换来的主要收益：此前续页只有一个孤零零的数据行，读者
// 看不到列的含义。
func TestBuildTableRepeatsHeaderOnContinuation(t *testing.T) {
	rows := make([][]Cell, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, []Cell{{{Text: "行"}}, {{Text: "值"}}})
	}
	document, err := Build(&Document{Blocks: []Block{tableOfCells(
		[]Cell{{{Text: "名称"}}, {{Text: "数量"}}}, rows,
	)}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	pages := expandDocument(t, document, DefaultOptions())
	if len(pages) < 2 {
		t.Fatalf("120 行表格应分多页，实际 %d 页", len(pages))
	}
	for index, page := range pages {
		header := false
		for _, value := range pageTextValues(page) {
			if value == "名称" || value == "数量" {
				header = true
			}
		}
		if !header {
			t.Errorf("第 %d 页没有重复表头", index+1)
		}
	}
}

// 同一页上不应出现两段表格：分片后每页只放一段，表头才不会在同一页里出现两次。
func TestBuildTablePlacesOneFragmentPerPage(t *testing.T) {
	rows := make([][]Cell, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, []Cell{{{Text: "行"}}, {{Text: "值"}}})
	}
	document, err := Build(&Document{Blocks: []Block{tableOfCells(
		[]Cell{{{Text: "名称"}}, {{Text: "数量"}}}, rows,
	)}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	pages := expandDocument(t, document, DefaultOptions())
	for index, page := range pages {
		count := 0
		for _, value := range pageTextValues(page) {
			if value == "名称" {
				count++
			}
		}
		if count > 1 {
			t.Errorf("第 %d 页出现了 %d 次表头，同一页只应放一段表格", index+1, count)
		}
	}
}

// 表格不能越过版心底边。
func TestBuildTableStaysInsideContent(t *testing.T) {
	options := DefaultOptions()
	rows := make([][]Cell, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, []Cell{{{Text: "行"}}, {{Text: "值"}}})
	}
	document, err := Build(&Document{Blocks: []Block{tableOfCells(
		[]Cell{{{Text: "名称"}}, {{Text: "数量"}}}, rows,
	)}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	pages := expandDocument(t, document, options)
	for index, page := range pages {
		bottom := options.PageHeight - options.MarginBottom
		for _, item := range pageItems(page) {
			var box *models.StBox
			switch item.Kind {
			case models.PageItemPath:
				if item.Path != nil {
					value := item.Path.Boundary
					box = &value
				}
			case models.PageItemText:
				if item.Text != nil {
					value := item.Text.Boundary
					box = &value
				}
			}
			if box == nil {
				continue
			}
			if box.Y+box.Height > bottom+0.5 {
				t.Errorf("第 %d 页有图元越过版心底边 %.2fmm（底边 %.2f）",
					index+1, box.Y+box.Height, bottom)
			}
		}
	}
}
