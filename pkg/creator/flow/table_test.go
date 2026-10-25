package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

// 合并行组保持同页，多行表头整组重复，最后的正文跟随完整表格。
func TestMergedTableGroupsStayTogetherAndRepeatMultirowHeaders(t *testing.T) {
	opts := smallOptions()
	d := flow.New(opts)
	d.AddParagraph("Before\nBefore\nBefore")
	grid := [][]flow.TableCell{
		{{Text: "Group", RowSpan: 2}, {Text: "Measures", ColSpan: 2}},
		{{Text: "First"}, {Text: "Second"}},
	}
	for i := 0; i < 7; i++ {
		grid = append(grid, []flow.TableCell{{Text: fmt.Sprintf("Group-%d", i), RowSpan: 2}, {Text: fmt.Sprintf("A-%d", i)}, {Text: fmt.Sprintf("B-%d", i)}},
			[]flow.TableCell{{Text: fmt.Sprintf("C-%d", i)}, {Text: fmt.Sprintf("D-%d", i)}})
	}
	d.AddTable(flow.Table{Grid: grid, HeaderRows: 2, ColumnWidths: []float64{20, 30, 30}})
	d.AddParagraph("Following paragraph")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 3 {
		t.Fatalf("页数过少: %d", len(model.Pages))
	}
	seen := make([]int, 7)
	for _, page := range model.Pages {
		text := pageText(page)
		if strings.Contains(text, "Group-") {
			for _, header := range []string{"Group\n", "Measures\n", "First\n", "Second\n"} {
				if strings.Count(text, header) != 1 {
					t.Fatalf("续页表头不完整: %q", text)
				}
			}
		}
		for i := range seen {
			if strings.Contains(text, fmt.Sprintf("Group-%d\n", i)) {
				seen[i]++
				for _, prefix := range []string{"A", "B", "C", "D"} {
					if !strings.Contains(text, fmt.Sprintf("%s-%d\n", prefix, i)) {
						t.Fatalf("合并行组被拆开: %q", text)
					}
				}
			}
		}
		bottom := 0.0
		for _, item := range page.Items {
			if path, ok := item.(creator.Path); ok {
				bottom = math.Max(bottom, path.Y+path.Height)
			}
			if text, ok := item.(creator.Text); ok {
				if text.Y < opts.MarginTop-0.001 || text.Y+text.Height > opts.PageSize.Height-opts.MarginBottom+0.001 {
					t.Fatalf("文字越出版心: %+v", text)
				}
				if textValue(text) == "Following paragraph" && text.Y < bottom-0.001 {
					t.Fatal("后续正文与表格重叠")
				}
			}
		}
	}
	for i, count := range seen {
		if count != 1 {
			t.Fatalf("第 %d 组合并内容出现 %d 次", i, count)
		}
	}
	var output bytes.Buffer
	if err := d.Write(context.Background(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(output.Bytes()), "merged.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("合并表格严格校验失败: %+v", report.Issues)
	}
}

func TestMergedCellsDoNotDrawInternalGridLines(t *testing.T) {
	for _, test := range []struct {
		name     string
		grid     [][]flow.TableCell
		vertical bool
	}{
		{"rowspan", [][]flow.TableCell{{{Text: "Merged", RowSpan: 2}, {Text: "Top"}}, {{Text: "Bottom"}}}, false},
		{"colspan", [][]flow.TableCell{{{Text: "Merged", ColSpan: 2}}, {{Text: "Left"}, {Text: "Right"}}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := flow.New(smallOptions())
			d.AddTable(flow.Table{Grid: test.grid, ColumnWidths: []float64{40, 40}})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var paths []creator.Path
			for _, item := range model.Pages[0].Items {
				if p, ok := item.(creator.Path); ok {
					paths = append(paths, p)
				}
			}
			if test.vertical {
				for _, p := range paths {
					if p.Width < 0.3 && p.Y < 18 && math.Abs(p.X-50) < 0.3 {
						t.Fatalf("跨列单元格内出现竖线: %+v", p)
					}
				}
			} else {
				var tall creator.Path
				for _, p := range paths {
					if math.Abs(p.X-10) < 0.001 && p.Width < 0.3 && p.Height > tall.Height {
						tall = p
					}
				}
				if tall.Height < 10 {
					t.Fatal("跨行边框未覆盖两行")
				}
				middle := tall.Y + tall.Height/2
				for _, p := range paths {
					if p.Height < 0.3 && p.X < 49.8 && math.Abs(p.Y-middle) < 0.3 {
						t.Fatalf("跨行单元格内出现横线: %+v", p)
					}
				}
			}
		})
	}
}

func TestMergedCellRichTextAndHeightUseActualFont(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{ColumnWidths: []float64{40, 40}, Grid: [][]flow.TableCell{
		{{RowSpan: 2, Runs: []flow.Run{{Text: "Measured ", Bold: true}, {Text: strings.Repeat("linked text ", 5), Href: "https://example.com"}}, Style: &flow.TextStyle{Size: 14, SpaceBefore: 2, SpaceAfter: 2}}, {Text: "Top"}},
		{{Text: "Bottom"}},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bold, link := false, false
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok {
			if strings.Contains(textValue(text), "Measured") {
				bold = text.Font == "Flow-Bold"
			}
			if len(text.Actions) > 0 && text.Actions[0].URI != nil {
				link = true
			}
			if text.Y+text.Height > 80.001 {
				t.Fatal("合并单元格文字溢出页底")
			}
		}
	}
	if !bold || !link {
		t.Fatalf("单元格局部样式丢失: bold=%v link=%v", bold, link)
	}
}

func TestMergedTableRejectsInvalidGrid(t *testing.T) {
	for _, test := range []struct {
		name  string
		table flow.Table
		want  string
	}{
		{"mixed", flow.Table{Rows: [][]string{{"x"}}, Grid: [][]flow.TableCell{{{Text: "x"}}}}, "不能同时"},
		{"negative", flow.Table{Grid: [][]flow.TableCell{{{Text: "x", ColSpan: -1}}}}, "不能为负数"},
		{"overflow", flow.Table{ColumnWidths: []float64{80}, Grid: [][]flow.TableCell{{{Text: "x", RowSpan: 2}}}}, "超出网格"},
		{"gap", flow.Table{ColumnWidths: []float64{40, 40}, Grid: [][]flow.TableCell{{{Text: "x"}}}}, "未被单元格覆盖"},
		{"overlap", flow.Table{Grid: [][]flow.TableCell{{{Text: "x"}, {Text: "y", RowSpan: 2}, {Text: "z"}}, {{Text: "overlap", ColSpan: 2}, {Text: "last"}}}}, "重叠"},
		{"header crossing", flow.Table{HeaderRows: 1, Grid: [][]flow.TableCell{{{Text: "x", RowSpan: 2}, {Text: "y"}}, {{Text: "z"}}}}, "表头与正文"},
		{"too many headers", flow.Table{HeaderRows: 2, Grid: [][]flow.TableCell{{{Text: "x"}}}}, "超出表格行数"},
		{"mixed cell content", flow.Table{Grid: [][]flow.TableCell{{{Text: "x", Runs: []flow.Run{{Text: "y"}}}}}}, "不能同时"},
		{"unknown cell font", flow.Table{Grid: [][]flow.TableCell{{{Style: &flow.TextStyle{Font: "unknown"}}}}}, "未注册"},
		{"oversize group", flow.Table{Grid: [][]flow.TableCell{{{RowSpan: 2, Text: strings.Repeat("line\n", 30)}, {Text: "x"}}, {{Text: "y"}}}}, "行组"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := flow.New(smallOptions())
			d.AddTable(test.table)
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误 = %v，期望包含 %q", err, test.want)
			}
		})
	}
}

func TestFullyCoveredRowsAndTransitiveRowspansStayTogether(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Grid: [][]flow.TableCell{{{Text: "Full", ColSpan: 2, RowSpan: 2}}, {}}, ColumnWidths: []float64{40, 40}})
	d.AddTable(flow.Table{Grid: [][]flow.TableCell{
		{{Text: "A", RowSpan: 2}, {Text: "a"}},
		{{Text: "B", RowSpan: 2}},
		{{Text: "c"}},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := pageText(model.Pages[0])
	for _, word := range []string{"Full", "A", "B", "c"} {
		if !strings.Contains(text, word+"\n") {
			t.Fatalf("合并内容丢失: %q", text)
		}
	}
}
