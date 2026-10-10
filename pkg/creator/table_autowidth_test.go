package creator

import (
	"math"
	"testing"
)

// autoWidthTable 造一张三列的表：序号短、名称长、金额中等，且不指定列宽。
func autoWidthTable(width float64, cells [][3]string) Table {
	rows := make([]TableRow, 0, len(cells))
	for _, row := range cells {
		rows = append(rows, TableRow{Cells: []TableCell{
			{Value: row[0], Size: 4.2},
			{Value: row[1], Size: 4.2},
			{Value: row[2], Size: 4.2},
		}})
	}
	return Table{
		X: 20, Y: 30, Width: width,
		HeaderRows: 1,
		Rows:       rows,
		Style:      TableStyle{HorizontalLines: true, VerticalLines: true},
	}
}

// sumColumnWidths 返回列宽合计。
func sumColumnWidths(layout *tableLayout) float64 {
	total := 0.0
	for _, width := range layout.colWidths {
		total += width
	}
	return total
}

// 不给 ColumnWidths 时按内容分配列宽，而不是均分。
//
// 均分下「序号」这种两字列会空掉一半宽度，而「上海市税务局第一税务稽查局」这种
// 长列被折成三四行，读起来是错位的。
func TestAutoColumnWidthsFollowContent(t *testing.T) {
	table := autoWidthTable(170, [][3]string{
		{"序号", "名称", "2026年10月10日"},
		{"1", "上海市税务局第一税务稽查局", "26630.48"},
		{"22", "短名", "0.11"},
	})
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	if layout.colWidths[1] <= layout.colWidths[0] {
		t.Errorf("长内容列应比短内容列宽，实际 %v", layout.colWidths)
	}
	if layout.colWidths[1] <= layout.colWidths[2] {
		t.Errorf("最长内容所在列应最宽，实际 %v", layout.colWidths)
	}
	// 名称列要放得下最长那行，否则自动列宽没有意义。
	longest := estimateTextWidth("上海市税务局第一税务稽查局", 4.2) + 2*defaultTableCellPadding
	if layout.colWidths[1]+0.01 < longest {
		t.Errorf("名称列宽 %.2fmm 放不下最长内容 %.2fmm", layout.colWidths[1], longest)
	}
}

// 列宽合计恒等于 Table.Width：列边界是对外承诺的，不能因为自动而漂移。
func TestAutoColumnWidthsFillDeclaredWidth(t *testing.T) {
	for _, width := range []float64{20, 85, 170, 400} {
		table := autoWidthTable(width, [][3]string{
			{"序号", "名称", "金额"},
			{"1", "上海市税务局第一税务稽查局", "26630.48"},
		})
		layout, err := layoutTable(table, 210, 297)
		if err != nil {
			t.Fatalf("Width=%.0f: layoutTable 失败: %v", width, err)
		}
		if got := sumColumnWidths(layout); math.Abs(got-width) > 0.01 {
			t.Errorf("Width=%.0f 时列宽合计 = %.2f", width, got)
		}
		if math.Abs(layout.colX[3]-(table.X+width)) > 0.01 {
			t.Errorf("Width=%.0f 时表格右边界 = %.2f, want %.2f", width, layout.colX[3], table.X+width)
		}
	}
}

// 内容远宽于表格时按比例压缩，但每列不能窄于一个不可断行的排版单元。
//
// 窄过这个下限，CJK 单字或 ASCII 单词会被劈开，断行算出的行数也对不上。
func TestAutoColumnWidthsShrinkButKeepFloor(t *testing.T) {
	table := autoWidthTable(40, [][3]string{
		{"序号", "名称", "2026年10月10日"},
		{"1", "上海市税务局第一税务稽查局", "26630.48"},
	})
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	floor := estimateTextWidth("稽", 4.2) + 2*defaultTableCellPadding
	for index, width := range layout.colWidths {
		if width+0.01 < floor {
			t.Errorf("第 %d 列宽 %.2fmm 低于单字下限 %.2fmm", index+1, width, floor)
		}
	}
	if got := sumColumnWidths(layout); math.Abs(got-40) > 0.01 {
		t.Errorf("压缩后列宽合计 = %.2f, want 40", got)
	}
}

// 连每列的不可断行下限都放不下时退回均分：此时按内容分配会让每列都不可用，
// 均分至少保证每列等宽、可折行。
func TestAutoColumnWidthsFallBackToEqualWhenFloorExceedsWidth(t *testing.T) {
	table := autoWidthTable(12, [][3]string{
		{"序号", "名称", "金额"},
		{"1", "上海市税务局第一税务稽查局", "26630.48"},
	})
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	want := 12.0 / 3
	for index, width := range layout.colWidths {
		if math.Abs(width-want) > 0.01 {
			t.Errorf("第 %d 列宽 = %.2f, want 均分 %.2f", index+1, width, want)
		}
	}
}

// 合并单元格把宽度需求均摊到它跨越的列上，而不是只算在首列。
func TestAutoColumnWidthsSpreadAcrossColSpan(t *testing.T) {
	build := func(span int) Table {
		rows := []TableRow{{Cells: []TableCell{
			{Value: "短", Size: 4.2},
			{Value: "也很短", Size: 4.2},
			{Value: "短", Size: 4.2},
		}}}
		if span > 0 {
			rows = append(rows, TableRow{Cells: []TableCell{
				{Value: "合并单元格标题写得很长很长", Size: 4.2, ColSpan: span},
			}})
		}
		return Table{X: 20, Y: 30, Width: 170, HeaderRows: 1, Rows: rows,
			Style: TableStyle{HorizontalLines: true, VerticalLines: true}}
	}
	plain, err := layoutTable(build(0), 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := layoutTable(build(2), 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	// 列宽按比例缩放到 Width，所以被跨越的那两列应当分到更大份额（从未跨越的
	// 那列那里匀出来），而不是把宽度全记在首列。
	spannedPlain := plain.colWidths[0] + plain.colWidths[1]
	spannedMerged := merged.colWidths[0] + merged.colWidths[1]
	if spannedMerged <= spannedPlain {
		t.Errorf("合并单元格跨越的两列应分到更大宽度，实际 %.2f vs %.2f（%v vs %v）",
			spannedMerged, spannedPlain, merged.colWidths, plain.colWidths)
	}
}

// Markdown 单元格按渲染后的文字量宽度，不能把语法符号算进去。
//
// 否则 “ `code` “、`**粗体**` 这类标记会凭空撑宽一列，而渲染出来的文字远没那么长。
func TestAutoColumnWidthsIgnoreMarkdownSyntax(t *testing.T) {
	build := func(first string, format string) *tableLayout {
		rows := []TableRow{
			{Cells: []TableCell{{Value: "列", Size: 4.2}, {Value: "值", Size: 4.2}}},
			{Cells: []TableCell{{Value: first, Size: 4.2, Format: format}, {Value: "1234", Size: 4.2}}},
		}
		layout, err := layoutTable(Table{X: 20, Y: 30, Width: 170, HeaderRows: 1, Rows: rows,
			Style: TableStyle{HorizontalLines: true, VerticalLines: true}}, 210, 297)
		if err != nil {
			t.Fatal(err)
		}
		return layout
	}
	marked := build("**abcd**", "markdown")
	plain := build("abcd", "")
	if marked.colWidths[0] != plain.colWidths[0] {
		t.Errorf("**粗体** 标记被算进了宽度：%.2fmm，纯文字同为 %.2fmm",
			marked.colWidths[0], plain.colWidths[0])
	}
}

// 显式给出 ColumnWidths 时一字不改。
func TestAutoColumnWidthsNotUsedWhenSpecified(t *testing.T) {
	table := autoWidthTable(170, [][3]string{{"a", "bb", "ccc"}, {"1", "2", "3"}})
	table.ColumnWidths = []float64{10, 20, 140}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range table.ColumnWidths {
		if layout.colWidths[index] != want {
			t.Errorf("第 %d 列宽 = %.2f, want %.2f", index+1, layout.colWidths[index], want)
		}
	}
}

// 空列也要有宽度，否则它的边框线会和邻列叠在一起。
func TestAutoColumnWidthsGiveEmptyColumnsPadding(t *testing.T) {
	table := Table{
		X: 20, Y: 30, Width: 170,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "甲", Size: 4.2}, {Value: "", Size: 4.2}, {Value: "丙", Size: 4.2}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	if layout.colWidths[1] < 2*defaultTableCellPadding {
		t.Errorf("空列宽 %.2fmm 不足以放下内边距", layout.colWidths[1])
	}
	if got := sumColumnWidths(layout); math.Abs(got-170) > 0.01 {
		t.Errorf("列宽合计 = %.2f, want 170", got)
	}
}

// 表头行按表头字号量宽度：表头字号通常比正文大，量小了表头就会被折行。
func TestAutoColumnWidthsUseHeaderSize(t *testing.T) {
	rows := []TableRow{
		{Cells: []TableCell{{Value: "甲乙丙丁", Size: 4.2}, {Value: "戊", Size: 4.2}}},
		{Cells: []TableCell{{Value: "戊己庚辛", Size: 4.2}, {Value: "壬", Size: 4.2}}},
	}
	style := TableStyle{HorizontalLines: true, VerticalLines: true, HeaderSize: 8}
	layout, err := layoutTable(Table{X: 20, Y: 30, Width: 170, HeaderRows: 1, Rows: rows, Style: style}, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	want := estimateTextWidth("甲乙丙丁", 8) + 2*defaultTableCellPadding
	if layout.colWidths[0] < want-0.01 {
		t.Errorf("首列宽 %.2fmm 放不下按表头字号量的 %.2fmm", layout.colWidths[0], want)
	}
}
