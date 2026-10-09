package creator

import (
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
		X:     20,
		Y:     30,
		Width: 170,
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
		X:     20,
		Y:     30,
		Width: 170,
		Caption: "Table 13 File Properties",
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
			err := validateTable(tt.table)
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
		X:     20,
		Y:     30,
		Width: 170,
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
		X:     20,
		Y:     30,
		Width: 170,
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

	texts := layout.generateCellTexts(tableSegment{startRow: 0, endRow: 2, isFirst: true})
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
			name:  "explicit newline",
			value: "第一行\n第二行",
			firstWidth: 200, contWidth: 200, size: 4.2,
			want: []string{"第一行", "第二行"},
		},
		{
			name:  "cjk auto wrap",
			value: "一二三四五六七八",
			firstWidth: 20, contWidth: 20, size: 4.2,
			want: []string{"一二三四", "五六七八"},
		},
		{
			name:  "ascii wrap at space",
			value: "hello world foo",
			firstWidth: 20, contWidth: 20, size: 4.2,
			want: []string{"hello", "world foo"},
		},
		{
			name:  "empty",
			value: "",
			firstWidth: 20, contWidth: 20, size: 4.2,
			want: []string{""},
		},
		{
			name:  "leading space kept",
			value: "ab  cd",
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

	texts := layout.createCellTexts(TableCell{Value: "普通**粗体**", Format: "markdown", Size: 4.2}, 20, 30, 170, 9.3, defaultTableCellPadding, 0)
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

	noIndent := layout.createCellTexts(TableCell{Value: "x", Size: 4.2}, 20, 30, 170, 9.3, defaultTableCellPadding, 0)
	withIndent := layout.createCellTexts(TableCell{Value: "x", Size: 4.2, FirstLineIndent: 8}, 20, 30, 170, 9.3, defaultTableCellPadding, 0)

	if len(noIndent) != 1 || len(withIndent) != 1 {
		t.Fatalf("expected single line, got %d and %d", len(noIndent), len(withIndent))
	}
	if withIndent[0].X-noIndent[0].X != 8 {
		t.Errorf("expected first line indent 8mm, got %.2f", withIndent[0].X-noIndent[0].X)
	}
}