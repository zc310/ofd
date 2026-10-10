package creator

import (
	"math"
	"strings"
	"testing"
)

func TestLayoutTableBasic(t *testing.T) {
	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows: []TableRow{
			{
				Cells: []TableCell{
					{Value: "Name", Size: 4.2},
					{Value: "Type", Size: 4.2},
					{Value: "Size", Size: 4.2},
				},
			},
			{
				Cells: []TableCell{
					{Value: "a.txt", Size: 4.2},
					{Value: "Text", Size: 4.2},
					{Value: "10KB", Size: 4.2},
				},
			},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
			LineWidth:       0.3,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	if layout.numCols != 3 {
		t.Errorf("expected 3 columns, got %d", layout.numCols)
	}
	if len(layout.rowHeights) != 2 {
		t.Errorf("expected 2 row heights, got %d", len(layout.rowHeights))
	}
	if len(layout.segments) != 1 {
		t.Errorf("expected 1 segment (no page break), got %d", len(layout.segments))
	}
}

func TestLayoutTableWithColSpan(t *testing.T) {
	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows: []TableRow{
			{
				Cells: []TableCell{
					{Value: "Product Info", ColSpan: 2, Align: "center", Size: 4.2},
					{Value: "Qty", Size: 4.2},
					{Value: "Price", Align: "right", Size: 4.2},
				},
			},
			{
				Cells: []TableCell{
					{Value: "Item A", Size: 4.2},
					{Value: "Detail", Size: 4.2},
					{Value: "10", Align: "center", Size: 4.2},
					{Value: "100.00", Align: "right", Size: 4.2},
				},
			},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	if layout.numCols != 4 {
		t.Errorf("expected 4 columns, got %d", layout.numCols)
	}
	if !layout.occupied[0][0] || !layout.occupied[0][1] {
		t.Error("expected row 0 col 0-1 to be occupied by ColSpan")
	}
}

func TestLayoutTableWithRowSpan(t *testing.T) {
	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows: []TableRow{
			{
				Cells: []TableCell{
					{Value: "Category A", RowSpan: 2, Align: "center", Size: 4.2},
					{Value: "Item A1", Size: 4.2},
					{Value: "10", Size: 4.2},
				},
			},
			{
				Cells: []TableCell{
					{Value: "Item A2", Size: 4.2},
					{Value: "20", Size: 4.2},
				},
			},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	if !layout.occupied[0][0] || !layout.occupied[1][0] {
		t.Error("expected col 0 rows 0-1 to be occupied by RowSpan")
	}
	if !layout.occupied[1][1] {
		t.Error("expected row 1 col 1 to be occupied by cell")
	}
}

func TestLayoutTablePageBreak(t *testing.T) {
	var rows []TableRow
	for i := 0; i < 100; i++ {
		rows = append(rows, TableRow{
			Cells: []TableCell{
				{Value: "Item", Size: 4.2},
				{Value: "Data", Size: 4.2},
			},
		})
	}

	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows:  rows,
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	if len(layout.segments) < 2 {
		t.Errorf("expected multiple segments for page break, got %d", len(layout.segments))
	}
	if !layout.segments[0].isFirst {
		t.Error("expected first segment to be marked as first")
	}
	for i := 1; i < len(layout.segments); i++ {
		if !layout.segments[i].isContinuation {
			t.Errorf("expected segment %d to be marked as continuation", i)
		}
	}
}

func TestLayoutTableWithCaption(t *testing.T) {
	table := Table{
		X:       20,
		Y:       30,
		Width:   170,
		Caption: "Table 13 File Properties",
		CaptionStyle: &CaptionStyle{
			Font:   "SimHei",
			Size:   5.0,
			Weight: 700,
			Align:  "center",
		},
		Rows: []TableRow{
			{
				Cells: []TableCell{
					{Value: "Name", Size: 4.2},
					{Value: "Value", Size: 4.2},
				},
			},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	caption := layout.generateCaption(tableSegment{isFirst: true})
	if caption == nil {
		t.Fatal("expected caption to be generated")
	}
	if caption.Value != "Table 13 File Properties" {
		t.Errorf("expected caption 'Table 13 File Properties', got %q", caption.Value)
	}
}

func TestLayoutTableContinuationCaption(t *testing.T) {
	table := Table{
		X:                   20,
		Y:                   30,
		Width:               170,
		Caption:             "Table 13 File Properties",
		ContinuationCaption: "{caption} (cont.)",
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "A", Size: 4.2}}},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	caption := layout.generateCaption(tableSegment{isContinuation: true})
	if caption == nil {
		t.Fatal("expected continuation caption to be generated")
	}
	if caption.Value != "Table 13 File Properties (cont.)" {
		t.Errorf("expected 'Table 13 File Properties (cont.)', got %q", caption.Value)
	}
}

func TestGenerateBorderPaths(t *testing.T) {
	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "A", Size: 4.2}, {Value: "B", Size: 4.2}}},
			{Cells: []TableCell{{Value: "C", Size: 4.2}, {Value: "D", Size: 4.2}}},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
			LineWidth:       0.3,
			OuterLineWidth:  0.8,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	paths := layout.generateBorderPaths(tableSegment{startRow: 0, endRow: 2, isFirst: true})
	if len(paths) == 0 {
		t.Error("expected border paths to be generated")
	}

	hasThickBorder := false
	for _, p := range paths {
		if p.LineWidth == 0.8 {
			hasThickBorder = true
			break
		}
	}
	if !hasThickBorder {
		t.Error("expected at least one outer border path with width 0.8")
	}
}

func TestGenerateBorderPathsMergedCells(t *testing.T) {
	table := Table{
		X:          20,
		Y:          30,
		Width:      170,
		HeaderRows: 1,
		Rows: []TableRow{
			{Cells: []TableCell{
				{Value: "A", ColSpan: 2, Size: 4.2},
				{Value: "B", Size: 4.2},
			}},
			{Cells: []TableCell{
				{Value: "C", RowSpan: 2, Size: 4.2},
				{Value: "D", Size: 4.2},
				{Value: "E", Size: 4.2},
			}},
			{Cells: []TableCell{
				{Value: "F", Size: 4.2},
				{Value: "G", Size: 4.2},
			}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	// 表头 "A" 跨列 0-1，列边界 1 在表头行不应绘制。
	if layout.needVerticalSegment(0, 1) {
		t.Error("expected header ColSpan to suppress internal vertical segment at col 1")
	}
	// "C" 跨行 1-2，行边界 2 在列 0 不应绘制。
	if layout.needHorizontalSegment(2, 0) {
		t.Error("expected RowSpan to suppress internal horizontal segment at col 0")
	}
	// 表头行其它列边界照常绘制。
	if !layout.needVerticalSegment(0, 2) {
		t.Error("expected normal vertical segment at col 2")
	}
	// 跨列单元格未覆盖的行，列边界 1 照常绘制。
	if !layout.needVerticalSegment(1, 1) {
		t.Error("expected normal vertical segment at row 1 col 1")
	}
}

func TestValidateTable(t *testing.T) {
	tests := []struct {
		name    string
		table   Table
		wantErr bool
	}{
		{
			name: "valid table",
			table: Table{
				Width: 170,
				Rows:  []TableRow{{Cells: []TableCell{{Value: "A", ColSpan: 1, RowSpan: 1}}}},
			},
			wantErr: false,
		},
		{
			name: "empty rows",
			table: Table{
				Width: 170,
				Rows:  []TableRow{},
			},
			wantErr: true,
		},
		{
			name: "empty cells in row",
			table: Table{
				Width: 170,
				Rows:  []TableRow{{Cells: []TableCell{}}},
			},
			wantErr: true,
		},
		{
			name: "invalid ColSpan",
			table: Table{
				Width: 170,
				Rows:  []TableRow{{Cells: []TableCell{{Value: "A", ColSpan: -1}}}},
			},
			wantErr: true,
		},
		{
			name: "invalid RowSpan",
			table: Table{
				Width: 170,
				Rows:  []TableRow{{Cells: []TableCell{{Value: "A", RowSpan: -1}}}},
			},
			wantErr: true,
		},
		{
			name: "header rows out of range",
			table: Table{
				Width:      170,
				HeaderRows: 5,
				Rows:       []TableRow{{Cells: []TableCell{{Value: "A"}}}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTable(tt.table, 1, nil, nil, nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEstimateTextWidth(t *testing.T) {
	tests := []struct {
		text string
		size float64
		want float64
	}{
		{"", 4.2, 0},
		{"ABC", 4.2, 6.3},
		{"中文", 4.2, 8.4},
		{"A中B", 4.2, 8.4},
	}

	for _, tt := range tests {
		got := estimateTextWidth(tt.text, tt.size)
		if got < tt.want-0.01 || got > tt.want+0.01 {
			t.Errorf("estimateTextWidth(%q, %.1f) = %.2f, want %.2f", tt.text, tt.size, got, tt.want)
		}
	}
}

func TestLayoutTableWithCustomColumnWidths(t *testing.T) {
	table := Table{
		X:            20,
		Y:            30,
		Width:        170,
		ColumnWidths: []float64{50, 60, 60},
		Rows: []TableRow{
			{
				Cells: []TableCell{
					{Value: "A", Size: 4.2},
					{Value: "B", Size: 4.2},
					{Value: "C", Size: 4.2},
				},
			},
		},
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	if len(layout.colWidths) != 3 {
		t.Fatalf("expected 3 column widths, got %d", len(layout.colWidths))
	}
	if layout.colWidths[0] != 50 || layout.colWidths[1] != 60 || layout.colWidths[2] != 60 {
		t.Errorf("unexpected column widths: %v", layout.colWidths)
	}
	if layout.colX[3] != 190 {
		t.Errorf("expected table right edge at 190, got %.2f", layout.colX[3])
	}
}

func TestGenerateCellTexts(t *testing.T) {
	table := Table{
		X:          20,
		Y:          30,
		Width:      170,
		HeaderRows: 1,
		Style: TableStyle{
			HorizontalLines: true,
			VerticalLines:   true,
			HeaderSize:      5.0,
			HeaderWeight:    700,
		},
		Rows: []TableRow{
			{
				Cells: []TableCell{
					{Value: "Name", Align: "center", Size: 4.2},
					{Value: "Value", Align: "right", Size: 4.2},
				},
			},
			{
				Cells: []TableCell{
					{Value: "a.txt", Size: 4.2},
					{Value: "100KB", Align: "right", Size: 4.2},
				},
			},
		},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	texts, _, _ := layout.generateCellTexts(tableSegment{startRow: 0, endRow: 2, isFirst: true})
	if len(texts) != 4 {
		t.Errorf("expected 4 cell texts, got %d", len(texts))
	}

	if texts[0].Weight != 700 {
		t.Errorf("expected header weight 700, got %d", texts[0].Weight)
	}
	if texts[0].Size != 5.0 {
		t.Errorf("expected header size 5.0, got %.2f", texts[0].Size)
	}
}

func TestWrapCellText(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		firstWidth float64
		contWidth  float64
		size       float64
		want       []string
	}{
		{
			name:       "explicit newline",
			value:      "第一行\n第二行",
			firstWidth: 200, contWidth: 200, size: 4.2,
			want: []string{"第一行", "第二行"},
		},
		{
			name:       "cjk auto wrap",
			value:      "一二三四五六七八",
			firstWidth: 20, contWidth: 20, size: 4.2,
			want: []string{"一二三四", "五六七八"},
		},
		{
			name:       "ascii wrap at space",
			value:      "hello world foo",
			firstWidth: 20, contWidth: 20, size: 4.2,
			want: []string{"hello", "world foo"},
		},
		{
			name:       "empty",
			value:      "",
			firstWidth: 20, contWidth: 20, size: 4.2,
			want: []string{""},
		},
		{
			name:       "leading space kept",
			value:      "ab  cd",
			firstWidth: 9, contWidth: 9, size: 4.2,
			want: []string{"ab", "  cd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapCellText(tt.value, tt.firstWidth, tt.contWidth, tt.size)
			if len(got) != len(tt.want) {
				t.Fatalf("wrapCellText(%q) = %q, want %q", tt.value, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("wrapCellText(%q)[%d] = %q, want %q", tt.value, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseMarkdownRuns(t *testing.T) {
	runs := parseMarkdownRuns("普通**粗体**普通")
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d: %+v", len(runs), runs)
	}
	if runs[0].text != "普通" || runs[0].bold {
		t.Errorf("run 0 = %+v", runs[0])
	}
	if runs[1].text != "粗体" || !runs[1].bold {
		t.Errorf("run 1 = %+v", runs[1])
	}
	if runs[2].text != "普通" || runs[2].bold {
		t.Errorf("run 2 = %+v", runs[2])
	}

	italic := parseMarkdownRuns("*斜体*")
	if len(italic) != 1 || italic[0].text != "斜体" || !italic[0].italic {
		t.Errorf("italic = %+v", italic)
	}

	code := parseMarkdownRuns("`code`")
	if len(code) != 1 || code[0].text != "code" || !code[0].code {
		t.Errorf("code = %+v", code)
	}
}

// markdownCellLines 走完整的"解析→断行"链路，返回单元格的各行文字。
func markdownCellLines(value string) []string {
	lines := wrapStyledRuns(cellRuns(TableCell{Value: value, Format: "markdown", Size: 4.2}), 500, 500, 4.2)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		var builder strings.Builder
		for _, run := range line {
			builder.WriteString(run.text)
		}
		out = append(out, builder.String())
	}
	return out
}

// Markdown 单元格里换行必须断行。软换行（源码里的单个换行）曾被渲染成空格，
// 多行内容会挤成一行，且与同单元格的纯文本格式（\n 即换行）不一致。
func TestMarkdownCellNewlineBreaksLine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  []string
	}{
		{"单换行", "第一行\n第二行", []string{"第一行", "第二行"}},
		{"CRLF", "第一行\r\n第二行", []string{"第一行", "第二行"}},
		{"硬换行（行尾两空格）", "第一行  \n第二行", []string{"第一行", "第二行"}},
		{"换行后样式继续", "**粗体**第一行\n第二行*斜体*", []string{"粗体第一行", "第二行斜体"}},
		{"强调跨行", "**粗\n体**", []string{"粗", "体"}},
		{"空行分段", "甲段\n\n乙段", []string{"甲段", "乙段"}},
		{"连续空行不产生空行", "甲\n\n\n乙", []string{"甲", "乙"}},
		{"首尾换行不产生空行", "\n甲\n", []string{"甲"}},
	} {
		got := markdownCellLines(tc.value)
		if len(got) != len(tc.want) {
			t.Errorf("%s：得到 %d 行 %q，期望 %d 行 %q", tc.name, len(got), got, len(tc.want), tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s：第 %d 行 = %q，期望 %q", tc.name, i+1, got[i], tc.want[i])
			}
		}
	}
}

// 无序列表渲染成带圆点的多行，有序列表渲染成带序号的多行，嵌套列表按层级缩进。
// 此前列表项被当作普通块依次摊平，"• " 之类的项目符号完全丢失。
func TestMarkdownCellList(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  []string
	}{
		{"无序列表", "- 甲\n- 乙", []string{"• 甲", "• 乙"}},
		{"星号无序", "* 甲\n* 乙", []string{"• 甲", "• 乙"}},
		{"加号无序", "+ 甲\n+ 乙", []string{"• 甲", "• 乙"}},
		{"有序列表", "1. 一\n2. 二", []string{"1. 一", "2. 二"}},
		{"有序列表自定义起始", "3. 三\n4. 四", []string{"3. 三", "4. 四"}},
		{"嵌套列表缩进", "- 甲\n  - 乙", []string{"• 甲", "  • 乙"}},
		{"列表项内强调", "- **粗**甲", []string{"• 粗甲"}},
		// 勾选状态是内容的一部分：丢掉后"- [x] 完成"与"- [ ] 待办"长得一样。
		{"任务列表", "- [x] 已完成\n- [ ] 待办", []string{"• [x] 已完成", "• [ ] 待办"}},
	} {
		got := markdownCellLines(tc.value)
		if len(got) != len(tc.want) {
			t.Errorf("%s：得到 %d 行 %q，期望 %d 行 %q", tc.name, len(got), got, len(tc.want), tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s：第 %d 行 = %q，期望 %q", tc.name, i+1, got[i], tc.want[i])
			}
		}
	}
}

// 列表项内的强调要保留样式：项目符号本身不是强调的一部分。
func TestMarkdownCellListMarkerIsNotStyled(t *testing.T) {
	runs := parseMarkdownRuns("- **粗**")
	if len(runs) < 2 {
		t.Fatalf("期望至少 2 个片段（符号 + 文本），实际 %d: %+v", len(runs), runs)
	}
	if runs[0].text != "• " || runs[0].bold {
		t.Errorf("项目符号片段 = %+v，应为不加粗的 \"• \"", runs[0])
	}
	found := false
	for _, run := range runs {
		if run.text == "粗" && run.bold {
			found = true
		}
	}
	if !found {
		t.Errorf("未找到加粗的\"粗\"片段: %+v", runs)
	}
}

// 多行单元格的行高要按行数算，否则文字会溢出到下一行。
func TestMarkdownCellHeightCountsAllLines(t *testing.T) {
	oneLine, err := layoutTable(Table{
		X: 20, Y: 30, Width: 170,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲", Format: "markdown", Size: 4.2}}}},
	}, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	multiLine, err := layoutTable(Table{
		X: 20, Y: 30, Width: 170,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲\n乙\n丙", Format: "markdown", Size: 4.2}}}},
	}, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	if multiLine.rowHeights[0] <= oneLine.rowHeights[0] {
		t.Errorf("三行单元格高 %.2fmm 未大于单行的 %.2fmm",
			multiLine.rowHeights[0], oneLine.rowHeights[0])
	}
}

func TestCreateCellTextsMarkdown(t *testing.T) {
	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "普通**粗体**", Format: "markdown", Size: 4.2}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	texts, _, _ := layout.createCellTexts(TableCell{Value: "普通**粗体**", Format: "markdown", Size: 4.2}, 20, 30, 170, 9.3, defaultTableCellPadding, 0)
	if len(texts) != 2 {
		t.Fatalf("expected 2 texts (normal + bold), got %d", len(texts))
	}
	if texts[0].Value != "普通" || texts[0].Weight == 700 {
		t.Errorf("text 0 = %+v", texts[0])
	}
	if texts[1].Value != "粗体" || texts[1].Weight != 700 {
		t.Errorf("text 1 = %+v", texts[1])
	}
	// 两个 Text 共享同一基线，X 依次累加。
	if texts[0].Y != texts[1].Y {
		t.Errorf("expected same baseline Y, got %.2f and %.2f", texts[0].Y, texts[1].Y)
	}
	if texts[1].X <= texts[0].X {
		t.Errorf("expected increasing X, got %.2f and %.2f", texts[0].X, texts[1].X)
	}
}

func TestCellIndentAffectsTextX(t *testing.T) {
	table := Table{
		X:     20,
		Y:     30,
		Width: 170,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "x", Size: 4.2}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}

	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable failed: %v", err)
	}

	noIndent, _, _ := layout.createCellTexts(TableCell{Value: "x", Size: 4.2}, 20, 30, 170, 9.3, defaultTableCellPadding, 0)
	withIndent, _, _ := layout.createCellTexts(TableCell{Value: "x", Size: 4.2, FirstLineIndent: 8}, 20, 30, 170, 9.3, defaultTableCellPadding, 0)

	if len(noIndent) != 1 || len(withIndent) != 1 {
		t.Fatalf("expected single line, got %d and %d", len(noIndent), len(withIndent))
	}
	if withIndent[0].X-noIndent[0].X != 8 {
		t.Errorf("expected first line indent 8mm, got %.2f", withIndent[0].X-noIndent[0].X)
	}
}

// strikeTestLayout 造一张单行表格用于取删除线。
func strikeTestLayout(t *testing.T, style TableStyle) *tableLayout {
	t.Helper()
	layout, err := layoutTable(Table{
		X: 20, Y: 30, Width: 170,
		Rows:  []TableRow{{Cells: []TableCell{{Value: "x", Size: 4.2}}}},
		Style: style,
	}, 210, 297)
	if err != nil {
		t.Fatalf("layoutTable 失败: %v", err)
	}
	return layout
}

// Markdown 删除线此前只解析不绘制：strike 一路传到 createCellTexts 却从未被使用，
// ~~删除~~ 与普通文字长得完全一样。OFD 的 TextObject 没有删除线属性，
// 正确做法是按被划文字的 X 与宽度另画横线。
func TestMarkdownStrikethroughDrawsLine(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     string
		wantLines int
	}{
		{"整段删除", "~~删除~~", 1},
		{"部分删除", "普通~~废弃~~普通", 1},
		{"多处删除", "甲~~乙~~丙~~丁~~戊", 2},
		{"无删除线", "普通文字", 0},
		{"与其他样式叠加", "**粗体**与~~删除~~", 1},
		{"列表项内", "- ~~已删除项~~", 1},
	} {
		layout := strikeTestLayout(t, TableStyle{})
		_, strikes, _ := layout.createCellTexts(
			TableCell{Value: tc.value, Format: "markdown", Size: 4.2},
			20, 30, 170, 9.3, defaultTableCellPadding, 0)
		if len(strikes) != tc.wantLines {
			t.Errorf("%s：得到 %d 条删除线，期望 %d", tc.name, len(strikes), tc.wantLines)
		}
	}
}

// 删除线必须与被划文字的 X 与宽度逐段对齐，否则线会盖错字或漏头露尾。
func TestMarkdownStrikethroughAlignsWithText(t *testing.T) {
	layout := strikeTestLayout(t, TableStyle{})
	texts, strikes, _ := layout.createCellTexts(
		TableCell{Value: "甲~~乙~~丙", Format: "markdown", Size: 4.2},
		20, 30, 170, 9.3, defaultTableCellPadding, 0)
	if len(texts) != 3 || len(strikes) != 1 {
		t.Fatalf("期望 3 段文字与 1 条删除线，实际 %d 段 %d 条", len(texts), len(strikes))
	}
	struck := texts[1]
	if struck.Value != "乙" {
		t.Fatalf("被划文字 = %q，期望 \"乙\"", struck.Value)
	}
	if strikes[0].X != struck.X {
		t.Errorf("删除线 X = %.2f，被划文字 X = %.2f，未对齐", strikes[0].X, struck.X)
	}
	if math.Abs(strikes[0].Width-struck.Width) > 0.001 {
		t.Errorf("删除线宽 %.2f 与文字宽 %.2f 不一致", strikes[0].Width, struck.Width)
	}
}

// 删除线要落在字身中部（基线上方），不能压到基线以下或高过字顶。
func TestMarkdownStrikethroughSitsOnGlyphBody(t *testing.T) {
	const size = 4.2
	layout := strikeTestLayout(t, TableStyle{})
	texts, strikes, _ := layout.createCellTexts(
		TableCell{Value: "~~字~~", Format: "markdown", Size: size},
		20, 30, 170, 9.3, defaultTableCellPadding, 0)
	if len(strikes) != 1 || len(texts) != 1 {
		t.Fatalf("期望 1 段文字与 1 条删除线，实际 %d 段 %d 条", len(texts), len(strikes))
	}
	baseline := texts[0].Y + texts[0].Height // 文字 Y 是框顶，基线在框底
	lineY := strikes[0].Y + strikes[0].Height/2
	height := baseline - lineY
	if height <= 0 || height >= size {
		t.Errorf("删除线距基线 %.2fmm，应在 0 与字号 %.2fmm 之间", height, size)
	}
	if want := size * strikeBaselineRatio; math.Abs(height-want) > 0.001 {
		t.Errorf("删除线位置 = %.2fmm，期望 %.2fmm", height, want)
	}
}

// 删除线要描边才画得出来；未指定颜色时与边框同色，指定时用指定色。
func TestMarkdownStrikethroughStyle(t *testing.T) {
	layout := strikeTestLayout(t, TableStyle{StrokeColor: &Color{R: 10, G: 20, B: 30}})
	_, strikes, _ := layout.createCellTexts(
		TableCell{Value: "~~字~~", Format: "markdown", Size: 4.2},
		20, 30, 170, 9.3, defaultTableCellPadding, 0)
	if len(strikes) != 1 {
		t.Fatalf("期望 1 条删除线，实际 %d 条", len(strikes))
	}
	if !strikes[0].Stroke {
		t.Error("删除线必须描边，否则不显示")
	}
	if strikes[0].LineWidth <= 0 {
		t.Errorf("删除线宽 = %.3f，应为正数", strikes[0].LineWidth)
	}
	if strikes[0].StrokeColor == nil || strikes[0].StrokeColor.R != 10 {
		t.Errorf("未指定 strike_color 时应沿用边框色，实际 %+v", strikes[0].StrokeColor)
	}

	custom := strikeTestLayout(t, TableStyle{StrikeColor: &Color{R: 200, G: 0, B: 0}})
	_, red, _ := custom.createCellTexts(
		TableCell{Value: "~~字~~", Format: "markdown", Size: 4.2},
		20, 30, 170, 9.3, defaultTableCellPadding, 0)
	if len(red) != 1 || red[0].StrokeColor == nil || red[0].StrokeColor.R != 200 {
		t.Errorf("指定 strike_color 后应使用该颜色，实际 %+v", red)
	}
}

// markdownRuleLines 走完整的解析与断行链路，返回各行的描述：分隔线记作 <RULE>。
func markdownRuleLines(value string) []string {
	lines := wrapStyledRuns(cellRuns(TableCell{Value: value, Format: "markdown", Size: 4.2}), 500, 500, 4.2)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) == 1 && line[0].rule {
			out = append(out, "<RULE>")
			continue
		}
		var builder strings.Builder
		for _, run := range line {
			builder.WriteString(run.text)
		}
		out = append(out, builder.String())
	}
	return out
}

// Markdown 分隔线（***）此前被 parseMarkdownRuns 整段丢弃，单元格里的分界消失。
// 它独占一行，且不产生多余空行。
func TestMarkdownThematicBreakTakesOwnLine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  []string
	}{
		{"单独分隔线", "***", []string{"<RULE>"}},
		{"夹在文字之间", "上段\n***\n下段", []string{"上段", "<RULE>", "下段"}},
		{"下划线写法", "上段\n___\n下段", []string{"上段", "<RULE>", "下段"}},
		{"空格分隔写法", "上段\n- - -\n下段", []string{"上段", "<RULE>", "下段"}},
		{"连续两条", "甲\n***\n乙\n___\n丙", []string{"甲", "<RULE>", "乙", "<RULE>", "丙"}},
		{"列表后分隔", "- 甲\n- 乙\n***\n尾注", []string{"• 甲", "• 乙", "<RULE>", "尾注"}},
		{"无分隔线", "普通文字", []string{"普通文字"}},
		// --- 紧跟文字时按 CommonMark 是 setext 标题下划线，不是分隔线，
		// 所以这里只该出现标题文字本身。
		{"文字后的三连横线是标题", "上段\n---\n下段", []string{"上段", "下段"}},
		{"行内三连横线", "文字---文字", []string{"文字---文字"}},
	} {
		got := markdownRuleLines(tc.value)
		if len(got) != len(tc.want) {
			t.Errorf("%s：得到 %d 行 %q，期望 %d 行 %q", tc.name, len(got), got, len(tc.want), tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s：第 %d 行 = %q，期望 %q", tc.name, i+1, got[i], tc.want[i])
			}
		}
	}
}

// 分隔线要画成横贯单元格内容区的横线，而不是宽度为零的空路径。
func TestMarkdownThematicBreakDrawsFullWidthLine(t *testing.T) {
	const cellWidth = 60.0
	layout := strikeTestLayout(t, TableStyle{})
	_, _, rules := layout.createCellTexts(
		TableCell{Value: "上段\n***\n下段", Format: "markdown", Size: 4.2},
		20, 30, cellWidth, 30, defaultTableCellPadding, 0)
	if len(rules) != 1 {
		t.Fatalf("期望 1 条分隔线，实际 %d 条", len(rules))
	}
	rule := rules[0]
	want := cellWidth - 2*defaultTableCellPadding
	if math.Abs(rule.Width-want) > 0.001 {
		t.Errorf("分隔线宽 %.2fmm，期望横贯内容区 %.2fmm", rule.Width, want)
	}
	if !rule.Stroke || rule.LineWidth <= 0 {
		t.Errorf("分隔线必须描边且线宽为正，实际 Stroke=%v LineWidth=%.3f", rule.Stroke, rule.LineWidth)
	}
	// 分隔线是块内分界，不该和表格边框争分量。
	if want := 4.2 * ruleLineWidthRatio; math.Abs(rule.LineWidth-want) > 0.0001 {
		t.Errorf("分隔线宽 %.4fmm，期望 %.4fmm", rule.LineWidth, want)
	}
	if rule.LineWidth >= strikeLineWidthRatio*4.2 {
		t.Errorf("分隔线宽 %.4fmm 不应粗于删除线", rule.LineWidth)
	}
	if rule.X < 20 || rule.X+rule.Width > 20+cellWidth {
		t.Errorf("分隔线 X %.2f 宽 %.2f 越出单元格", rule.X, rule.Width)
	}
}

// 未指定 rule_color 时用浅灰，指定时用指定色。
//
// 默认不沿用边框色是有意的：同色的分隔线看起来像多出来的一道框线，
// 读者会去找它围的是什么。
func TestMarkdownThematicBreakColor(t *testing.T) {
	layout := strikeTestLayout(t, TableStyle{StrokeColor: &Color{R: 10, G: 20, B: 30}})
	_, _, rules := layout.createCellTexts(
		TableCell{Value: "***", Format: "markdown", Size: 4.2},
		20, 30, 60, 20, defaultTableCellPadding, 0)
	if len(rules) != 1 || rules[0].StrokeColor == nil {
		t.Fatalf("期望 1 条带颜色的分隔线，实际 %+v", rules)
	}
	color := *rules[0].StrokeColor
	if color.R != ruleColorGray || color.G != ruleColorGray || color.B != ruleColorGray {
		t.Errorf("未指定 rule_color 时应为浅灰 %d，实际 %+v", ruleColorGray, color)
	}

	custom := strikeTestLayout(t, TableStyle{RuleColor: &Color{R: 0, G: 128, B: 0}})
	_, _, green := custom.createCellTexts(
		TableCell{Value: "***", Format: "markdown", Size: 4.2},
		20, 30, 60, 20, defaultTableCellPadding, 0)
	if len(green) != 1 || green[0].StrokeColor == nil || green[0].StrokeColor.G != 128 {
		t.Errorf("指定 rule_color 后应使用该颜色，实际 %+v", green)
	}
}

// 分隔线要占一行，行高按行数算，否则后面的文字会压上来。
func TestMarkdownThematicBreakAddsLineHeight(t *testing.T) {
	plain, err := layoutTable(Table{
		X: 20, Y: 30, Width: 170,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲", Format: "markdown", Size: 4.2}}}},
	}, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	withRule, err := layoutTable(Table{
		X: 20, Y: 30, Width: 170,
		Rows: []TableRow{{Cells: []TableCell{{Value: "甲\n***\n乙", Format: "markdown", Size: 4.2}}}},
	}, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	if withRule.rowHeights[0] <= plain.rowHeights[0] {
		t.Errorf("含分隔线的三行单元格高 %.2fmm 未大于单行的 %.2fmm",
			withRule.rowHeights[0], plain.rowHeights[0])
	}
}
