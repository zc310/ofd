package creator

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// 找出该段里填充色等于给定 RGB 的路径。
func fillsWithColor(layout *tableLayout, seg tableSegment, r, g, b uint8) []Path {
	var paths []Path
	for _, path := range layout.generateHeaderFills(seg) {
		if path.Fill && path.FillColor != nil &&
			path.FillColor.R == r && path.FillColor.G == g && path.FillColor.B == b {
			paths = append(paths, path)
		}
	}
	return paths
}

// 表头底色此前只是声明：模型、校验、manifest 转换三处都认 HeaderFillColor，
// 绘制侧却没有对应图元，于是声明了也没有任何效果。
func TestTableHeaderFillColorIsRendered(t *testing.T) {
	table := Table{
		X: 20, Y: 30, Width: 120,
		HeaderRows: 1,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "甲", Size: 4.2}, {Value: "乙", Size: 4.2}}},
			{Cells: []TableCell{{Value: "丙", Size: 4.2}, {Value: "丁", Size: 4.2}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true,
			HeaderFillColor: &Color{R: 238, G: 238, B: 242}},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	seg := layout.segments[0]
	fills := fillsWithColor(layout, seg, 238, 238, 242)
	if len(fills) != 2 {
		t.Fatalf("表头两个单元格应各有一条底色，实际 %d 条", len(fills))
	}
	// 底色要覆盖表头行整行高度，并横跨各自列宽。
	if math := fills[0].Height; math != layout.rowY[1]-layout.rowY[0] {
		t.Errorf("底色高 %.2fmm, want 表头行高 %.2fmm", math, layout.rowY[1]-layout.rowY[0])
	}
	if fills[0].X != layout.colX[0] || fills[0].Width != layout.colX[1]-layout.colX[0] {
		t.Errorf("首列底色横向范围 = %.2f..%.2f, want %.2f..%.2f",
			fills[0].X, fills[0].X+fills[0].Width, layout.colX[0], layout.colX[1])
	}
	if fills[1].X != layout.colX[1] {
		t.Errorf("次列底色左边界 = %.2f, want %.2f", fills[1].X, layout.colX[1])
	}
	// 底色是填充，不是描边。
	for index, path := range fills {
		if path.Stroke {
			t.Errorf("第 %d 条底色不应描边", index+1)
		}
	}
}

// 没声明底色时不应凭空多出填充路径。
func TestTableNoHeaderFillWithoutColor(t *testing.T) {
	table := Table{
		X: 20, Y: 30, Width: 120, HeaderRows: 1,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "甲", Size: 4.2}, {Value: "乙", Size: 4.2}}},
			{Cells: []TableCell{{Value: "丙", Size: 4.2}, {Value: "丁", Size: 4.2}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	if fills := layout.generateHeaderFills(layout.segments[0]); len(fills) != 0 {
		t.Errorf("未声明底色时不应生成填充，实际 %d 条", len(fills))
	}
}

// 表头行里的合并单元格：底色按单元格画，不能在合并区域上留缝，也不能按列
// 画成三条互相重叠的窄条。
func TestTableHeaderFillSpansMergedCell(t *testing.T) {
	table := Table{
		X: 20, Y: 30, Width: 120,
		HeaderRows: 2,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "跨两行两列", Size: 4.2, ColSpan: 2, RowSpan: 2}}},
			{Cells: []TableCell{{Value: "丙", Size: 4.2}, {Value: "丁", Size: 4.2}}},
		},
		Style: TableStyle{HorizontalLines: true, VerticalLines: true,
			HeaderFillColor: &Color{R: 200, G: 210, B: 220}},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	fills := fillsWithColor(layout, layout.segments[0], 200, 210, 220)
	if len(fills) != 1 {
		t.Fatalf("合并单元格应只有一条底色，实际 %d 条", len(fills))
	}
	if fills[0].Width != layout.colX[2]-layout.colX[0] {
		t.Errorf("合并底色宽 %.2fmm, want 跨越两列的 %.2fmm",
			fills[0].Width, layout.colX[2]-layout.colX[0])
	}
	if fills[0].Height != layout.rowY[2]-layout.rowY[0] {
		t.Errorf("合并底色高 %.2fmm, want 跨越两行的 %.2fmm",
			fills[0].Height, layout.rowY[2]-layout.rowY[0])
	}
}

// 数据行不能有底色：HeaderRows 之外的行号一律不画。
func TestTableHeaderFillStopsAtHeaderRows(t *testing.T) {
	table := Table{
		X: 20, Y: 30, Width: 120, HeaderRows: 1,
		Rows: []TableRow{
			{Cells: []TableCell{{Value: "表头", Size: 4.2}}},
			{Cells: []TableCell{{Value: "数据", Size: 4.2}}},
		},
		Style: TableStyle{HeaderFillColor: &Color{R: 1, G: 2, B: 3}},
	}
	layout, err := layoutTable(table, 210, 297)
	if err != nil {
		t.Fatal(err)
	}
	for _, fill := range layout.generateHeaderFills(layout.segments[0]) {
		if fill.Y >= layout.rowY[1] {
			t.Errorf("底色画到了数据行：Y=%.2f, 表头行底边 %.2f", fill.Y, layout.rowY[1])
		}
	}
}

// 端到端：产物里必须真的有底色图元，且通过严格校验。
//
// 这一条守的是最初的 bug——字段声明了、校验也过了、manifest 也转换了，唯独绘制侧
// 没有图元，只看模型与校验的实现根本发现不了。
func TestTableHeaderFillSurvivesToOFD(t *testing.T) {
	var output bytes.Buffer
	if err := CreateWithOptions(Document{
		ID: "headerfill", PageSize: A4,
		Pages: []Page{{Items: []Item{Table{
			X: 20, Y: 30, Width: 120, HeaderRows: 1,
			Rows: []TableRow{
				{Cells: []TableCell{{Value: "甲", Size: 4.2}, {Value: "乙", Size: 4.2}}},
				{Cells: []TableCell{{Value: "丙", Size: 4.2}, {Value: "丁", Size: 4.2}}},
			},
			Style: TableStyle{HorizontalLines: true, VerticalLines: true,
				HeaderFillColor: &Color{R: 238, G: 238, B: 242}},
		}}}},
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
	found := false
	for _, item := range page.Content().Layer[0].Items {
		if item.Kind != models.PageItemPath || item.Path == nil {
			continue
		}
		fill := item.Path.FillColor
		if fill == nil || fill.Value == nil {
			continue
		}
		if fill.Value.R == 238 && fill.Value.G == 238 && fill.Value.B == 242 {
			found = true
		}
	}
	if !found {
		t.Error("产物里没有表头底色图元：HeaderFillColor 声明了但没有落到 OFD")
	}
}
