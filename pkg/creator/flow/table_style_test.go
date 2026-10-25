package flow_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func TestAutoColumnsFitContentRespectBoundsAndKeepNumbersWhole(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{
		Columns: []flow.TableColumn{{Width: 14}, {MinWidth: 20, MaxWidth: 45, Weight: 3}, {MinWidth: 18, MaxWidth: 25, NoWrap: true, Align: "right"}},
		Header:  []string{"ID", "Description", "Amount"},
		Rows:    [][]string{{"A01", "A useful description that wraps into several lines", "12345.67"}, {"A02", "Short", "98.00"}},
	})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var widths []float64
	amounts := 0
	for _, item := range model.Pages[0].Items {
		if p, ok := item.(creator.Path); ok && math.Abs(p.Y-10) < 0.001 && math.Abs(p.Height-0.2) < 0.001 && p.Width > 1 {
			widths = append(widths, p.Width)
		}
		if text, ok := item.(creator.Text); ok && (textValue(text) == "12345.67" || textValue(text) == "98.00") {
			amounts++
			if math.Abs(text.X+text.Width-88.5) > 0.001 {
				t.Fatalf("金额未按右边距对齐: %+v", text)
			}
		}
	}
	if len(widths) != 3 || math.Abs(widths[0]-14) > 0.001 || widths[1] < 20 || widths[1] > 45.001 || widths[2] < 18 || widths[2] > 25.001 || math.Abs(widths[0]+widths[1]+widths[2]-80) > 0.001 || amounts != 2 {
		t.Fatalf("列宽或金额不符合约束: widths=%v amounts=%d", widths, amounts)
	}
}

func TestAutoColumnsHonorMergedCellsFontsAndExplicitNewlines(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{MinWidth: 10}, {MinWidth: 10}, {Width: 20, NoWrap: true}}, Grid: [][]flow.TableCell{
		{{ColSpan: 2, Runs: []flow.Run{{Text: "Merged ", Bold: true}, {Text: "description"}}}, {Text: "12.30\n45.60"}},
		{{Text: "First"}, {Text: "Second"}, {Text: "78.90"}},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := pageText(model.Pages[0])
	for _, word := range []string{"Merged", "description", "12.30", "45.60", "78.90"} {
		if !strings.Contains(text, word) {
			t.Fatalf("缺少内容 %q: %q", word, text)
		}
	}
	if d.Blocks[0].Table.Columns[0].MinWidth != 10 || d.Blocks[0].Table.Grid[0][0].Runs[0].Text != "Merged " {
		t.Fatal("测量修改了输入")
	}
}

func TestAutoColumnsShareWidthAcrossOverlappingMergedConstraints(t *testing.T) {
	d := flow.New(smallOptions())
	word := strings.Repeat("W", 16)
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{MinWidth: 10}, {MinWidth: 10}, {MinWidth: 10, MaxWidth: 11}}, Grid: [][]flow.TableCell{
		{{Text: word, ColSpan: 2}, {Text: "x"}},
		{{Text: "x"}, {Text: word, ColSpan: 2}},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && textValue(text) == word {
			count++
			if text.X+text.Width > 88.501 {
				t.Fatalf("合并区域文字越界: %+v", text)
			}
		}
	}
	if count != 2 {
		t.Fatalf("合并区域内容缺失: %d", count)
	}
}

func TestAutomaticColumnsDistributeSurplusByWeightAndStopAtMaximum(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{Weight: 1, MaxWidth: 12}, {Weight: 3}}, Rows: [][]string{{"", ""}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var widths []float64
	for _, item := range model.Pages[0].Items {
		if p, ok := item.(creator.Path); ok && p.Height == 0.2 && p.Width > 1 && math.Abs(p.Y-10) < 0.001 {
			widths = append(widths, p.Width)
		}
	}
	if len(widths) != 2 || math.Abs(widths[0]-12) > 0.001 || math.Abs(widths[1]-68) > 0.001 {
		t.Fatalf("带上限的比例分配错误: %v", widths)
	}
}

func TestAutomaticColumnRatiosRemainFiniteWithLargeWeights(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{Weight: 1e308}, {Weight: 1e308}}, Rows: [][]string{{"A", "B"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range model.Pages[0].Items {
		if p, ok := item.(creator.Path); ok && (math.IsNaN(p.X) || math.IsNaN(p.Width) || math.IsInf(p.Width, 0) || p.X+p.Width > 90.001) {
			t.Fatalf("比例计算产生非法坐标: %+v", p)
		}
	}
}

func TestCellAppearanceSupportsZeroPaddingVerticalAlignmentAndBorders(t *testing.T) {
	positions := map[string]float64{}
	fill := &creator.Color{R: 220, G: 240, B: 255}
	stripe := &creator.Color{R: 245, G: 245, B: 245}
	for _, align := range []string{"top", "middle", "bottom"} {
		d := flow.New(smallOptions())
		hidden := &flow.TableBorder{}
		d.AddTable(flow.Table{ColumnWidths: []float64{40, 40}, StripeFill: stripe,
			CellStyle: &flow.CellStyle{Padding: &flow.Margins{Top: 2, Right: 2, Bottom: 2, Left: 2}, Borders: &flow.CellBorders{Top: hidden, Right: hidden, Bottom: hidden, Left: hidden}},
			Grid: [][]flow.TableCell{
				{{Text: "Aligned", RowSpan: 2, Appearance: &flow.CellStyle{Fill: fill, Padding: &flow.Margins{}, VerticalAlign: align, Borders: &flow.CellBorders{Bottom: &flow.TableBorder{Width: 0.7}}}}, {Text: "First\nSecond\nThird"}},
				{{Text: "Last"}},
			},
		})
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		fills, stripes, borders := 0, 0, 0
		for _, item := range model.Pages[0].Items {
			if text, ok := item.(creator.Text); ok && textValue(text) == "Aligned" {
				positions[align] = text.Y
				if text.X != 10 {
					t.Fatalf("零内边距未生效: %g", text.X)
				}
			}
			if p, ok := item.(creator.Path); ok {
				switch p.FillColor {
				case fill:
					fills++
				case stripe:
					stripes++
				default:
					borders++
					if math.Abs(p.Height-0.7) > 0.001 {
						t.Fatalf("隐藏边框仍被绘制: %+v", p)
					}
				}
			}
		}
		if fills != 1 || stripes != 1 || borders != 1 {
			t.Fatalf("外观继承错误: fills=%d stripes=%d borders=%d", fills, stripes, borders)
		}
	}
	if positions["top"] >= positions["middle"] || positions["middle"] >= positions["bottom"] || math.Abs(2*positions["middle"]-positions["top"]-positions["bottom"]) > 0.001 {
		t.Fatalf("垂直对齐错误: %v", positions)
	}
}

func TestAutoColumnsAndCellAppearanceRejectInvalidSettings(t *testing.T) {
	for _, test := range []struct {
		name  string
		table flow.Table
		want  string
	}{
		{"mixed widths", flow.Table{ColumnWidths: []float64{80}, Columns: []flow.TableColumn{{}}}, "互斥"},
		{"negative width", flow.Table{Columns: []flow.TableColumn{{MinWidth: -1}}}, "有限非负数"},
		{"nan width", flow.Table{Columns: []flow.TableColumn{{Weight: math.NaN()}}}, "有限非负数"},
		{"fixed outside bounds", flow.Table{Columns: []flow.TableColumn{{Width: 10, MinWidth: 20}}}, "固定宽"},
		{"reversed bounds", flow.Table{Columns: []flow.TableColumn{{MinWidth: 20, MaxWidth: 10}}}, "min_width"},
		{"invalid alignment", flow.Table{Columns: []flow.TableColumn{{Align: "invalid"}}}, "对齐"},
		{"nowrap overflow", flow.Table{Columns: []flow.TableColumn{{MaxWidth: 12, NoWrap: true}}}, "上限"},
		{"minimum overflow", flow.Table{Columns: []flow.TableColumn{{MinWidth: 90}}}, "最小总宽"},
		{"padding", flow.Table{CellStyle: &flow.CellStyle{Padding: &flow.Margins{Left: -1}}}, "内边距"},
		{"vertical align", flow.Table{CellStyle: &flow.CellStyle{VerticalAlign: "center"}}, "vertical_align"},
		{"border", flow.Table{CellStyle: &flow.CellStyle{Borders: &flow.CellBorders{Left: &flow.TableBorder{Width: math.Inf(1)}}}}, "边框宽度"},
	} {
		t.Run(test.name, func(t *testing.T) {
			table := test.table
			table.Rows = [][]string{{"Long description"}}
			d := flow.New(smallOptions())
			d.AddTable(table)
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误=%v，期望 %q", err, test.want)
			}
		})
	}
}

func TestStripeRowsRemainStableAcrossPageBreaks(t *testing.T) {
	d := flow.New(smallOptions())
	stripe := &creator.Color{R: 240, G: 240, B: 240}
	rows := make([][]string, 18)
	for i := range rows {
		rows[i] = []string{"Row"}
	}
	d.AddTable(flow.Table{Header: []string{"Header"}, Rows: rows, StripeFill: stripe})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 2 {
		t.Fatal("未覆盖跨页")
	}
	count := 0
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if p, ok := item.(creator.Path); ok && p.FillColor == stripe {
				count++
			}
		}
	}
	if count != 9 {
		t.Fatalf("隔行底色数量错误: %d", count)
	}
}

func TestAutomaticColumnMinimumAccountsForActualBoldFont(t *testing.T) {
	// 全固定列的内容测量也必须使用粗体面，不能按常规面误判可容纳。
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{Width: 5}}, Grid: [][]flow.TableCell{{{Runs: []flow.Run{{Text: "WWW", Bold: true}}}}}})
	_, err := d.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("未检测真实字体越界: %v", err)
	}
}

func TestCellBorderPathsStayWithinContentArea(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{}, {}}, CellStyle: &flow.CellStyle{Borders: &flow.CellBorders{Top: &flow.TableBorder{Width: 100}}}, Rows: [][]string{{"A", "B"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var xs []float64
	for _, item := range model.Pages[0].Items {
		if p, ok := item.(creator.Path); ok {
			if p.X < 10 || p.X+p.Width > 90.001 || p.Y < 10 || p.Y+p.Height > 80.001 {
				t.Fatalf("边框越界: %+v", p)
			}
			xs = append(xs, p.X)
		}
	}
	if len(xs) == 0 {
		t.Fatal("缺少边框")
	}
}
