package flow_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/gofont/goregular"
)

func paginationLines(prefix string, count int) string {
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s%02d", prefix, i)
	}
	return strings.Join(lines, "\n")
}

func paginationCounts(doc *creator.Document, prefix string, columns int) [][]int {
	result := make([][]int, len(doc.Pages))
	for p, page := range doc.Pages {
		result[p] = make([]int, columns)
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok || !strings.HasPrefix(textValue(text), prefix) {
				continue
			}
			// smallOptions 的版心为 80mm，栏间距为 8mm。
			col := min(columns-1, int((text.X-10+0.001)/((80.0+8)/float64(columns))))
			result[p][col]++
		}
	}
	return result
}

func checkPaginationDocument(t *testing.T, d *flow.Document) *creator.Document {
	t.Helper()
	doc, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := creator.MarshalWithOptions(*doc, creator.CreateOptions{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	r := v.ValidateReader(context.Background(), bytes.NewReader(data), "pagination.ofd")
	if r.HasErrors() || r.HasWarnings() {
		t.Fatalf("严格校验失败: %+v", r.Issues)
	}
	again, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := creator.MarshalWithOptions(*again, creator.CreateOptions{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, second) {
		t.Fatal("重排结果不确定")
	}
	for _, text := range modelTexts(doc) {
		if text.Y < 10-0.001 || text.Y+text.Height > 80+0.001 {
			t.Fatalf("文字越出版心: %+v", text)
		}
	}
	for _, page := range doc.Pages {
		for _, item := range page.Items {
			if path, ok := item.(creator.Path); ok && (path.Y < 10-0.001 || path.Y+path.Height > 80+0.001) {
				t.Fatalf("表格边框或背景越出版心: %+v", path)
			}
		}
	}
	return doc
}

func TestSplitRowsUsesRemainingSpaceAndContinuesTallCell(t *testing.T) {
	for _, tc := range []struct{ prefix, lines int }{{0, 8}, {3, 8}, {0, 20}, {3, 60}} {
		t.Run(fmt.Sprintf("prefix%d-lines%d", tc.prefix, tc.lines), func(t *testing.T) {
			d := flow.New(smallOptions())
			if tc.prefix > 0 {
				d.AddParagraph(paginationLines("Prefix", tc.prefix))
			}
			d.Blocks = append(d.Blocks, flow.Block{ID: "table", Kind: "table", Table: &flow.Table{SplitRows: true, Header: []string{"Header"}, Rows: [][]string{{paginationLines("Cell", tc.lines)}}}})
			doc := checkPaginationDocument(t, d)
			counts := paginationCounts(doc, "Cell", 1)
			if counts[0][0] == 0 {
				t.Fatal("首个片段没有利用剩余空间")
			}
			seen := map[string]int{}
			for _, text := range modelTexts(doc) {
				seen[textValue(text)]++
			}
			for i := 0; i < tc.lines; i++ {
				if seen[fmt.Sprintf("Cell%02d", i)] != 1 {
					t.Fatalf("行丢失或重复: %d", i)
				}
			}
			if seen["Header"] != len(doc.Pages) || doc.Bookmarks[0].Goto.Page != 0 {
				t.Fatal("表头或表格起点错误")
			}
		})
	}
}

func TestBalanceLastPageUsesLegalLineBreaks(t *testing.T) {
	for _, tc := range []struct {
		lines, columns int
		want           [][]int
	}{
		{10, 2, [][]int{{5, 5}}}, {20, 2, [][]int{{10, 10}}}, {40, 2, [][]int{{16, 16}, {4, 4}}},
		{12, 3, [][]int{{4, 4, 4}}}, {16, 8, [][]int{{2, 2, 2, 2, 2, 2, 2, 2}}},
		{3, 2, [][]int{{3, 0}}}, // 默认前后至少两行，不能强行分成 2/1。
	} {
		t.Run(fmt.Sprintf("%d-lines-%d-columns", tc.lines, tc.columns), func(t *testing.T) {
			o := smallOptions()
			o.Columns = flow.Columns{Count: tc.columns, Gap: 8, Balance: true}
			o.Theme.Body.Size, o.Theme.Body.LineHeight, o.Theme.Body.SpaceAfter = 10, 1.2, 0
			if tc.columns == 8 {
				o.Theme.Body.Size = 3
			}
			d := flow.New(o)
			d.AddParagraph(paginationLines("L", tc.lines))
			doc := checkPaginationDocument(t, d)
			got := paginationCounts(doc, "L", tc.columns)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("分栏=%v，期望=%v", got, tc.want)
			}
		})
	}
}

func TestSplitRowsRejectsBodyRowSpanAndRemainsOptIn(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Rows: [][]string{{paginationLines("Cell", 30)}}})
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "行组") {
		t.Fatalf("默认整行策略改变: %v", err)
	}
	d.Blocks[0].Table = &flow.Table{SplitRows: true, Grid: [][]flow.TableCell{{{Text: "Merged", RowSpan: 2}}, {}}}
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "grid[0][0]: split_rows") {
		t.Fatalf("RowSpan 未明确拒绝: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Build(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
}

// 同一逻辑行的短列只输出一次；合并列、续表题、重复表头与脚注跟随实际片段。
func TestSplitRowsColSpanFootnotesAndBalancingShareFragments(t *testing.T) {
	for _, balanced := range []bool{false, true} {
		for _, align := range []string{"top", "middle", "bottom"} {
			t.Run(fmt.Sprintf("balance%v-%s", balanced, align), func(t *testing.T) {
				o := smallOptions()
				o.Columns = flow.Columns{Count: 2, Gap: 8, Balance: balanced}
				o.Footnotes = []flow.Footnote{{ID: "header", Text: "HNOTE"}, {ID: "late", Text: "LNOTE"}}
				d := flow.New(o)
				d.Blocks = append(d.Blocks, flow.Block{ID: "t", Kind: "table", Table: &flow.Table{
					SplitRows: true, HeaderRows: 1, Caption: "T", Numbered: true, RepeatCaption: true,
					CellStyle: &flow.CellStyle{VerticalAlign: align, Fill: &creator.Color{R: 245, G: 245, B: 245}},
					Grid: [][]flow.TableCell{
						{{ColSpan: 2, Runs: []flow.Run{{Text: "H"}, {Footnote: "header"}}}, {Text: "H2"}},
						{{ColSpan: 2, Runs: []flow.Run{{Text: paginationLines("R", 24) + "\nLATE"}, {Footnote: "late"}}}, {Text: "S"}},
						{{Text: "Tail", ColSpan: 3}},
					},
				}})
				d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "See "}, {CaptionRef: "t"}, {Text: " p"}, {PageRef: "t"}}})
				doc := checkPaginationDocument(t, d)
				if len(doc.Pages) < 2 {
					t.Fatal("未跨页续排")
				}
				seen := map[string]int{}
				for _, text := range modelTexts(doc) {
					seen[textValue(text)]++
				}
				if seen["S"] != 1 || seen["Tail"] != 1 || seen["LATE"] != 1 {
					t.Fatalf("不同长度的列重复或丢失: %v", seen)
				}
				for i := 0; i < 24; i++ {
					if seen[fmt.Sprintf("R%02d", i)] != 1 {
						t.Fatalf("合并列第 %d 行重复或丢失", i)
					}
				}
				lateNotes, headers := 0, 0
				for p, page := range doc.Pages {
					for col := 0; col < 2; col++ {
						values := map[string]int{}
						for _, item := range page.Items {
							text, ok := item.(creator.Text)
							if !ok {
								continue
							}
							c := 0
							if text.X >= 54 {
								c = 1
							}
							if c == col {
								values[textValue(text)]++
							}
							for _, action := range text.Actions {
								if action.Goto != nil && (action.Goto.Page < 0 || action.Goto.Page >= len(doc.Pages) || action.Goto.Type != "XYZ" || action.Goto.Left == nil) {
									t.Fatalf("片段导航无效 page%d: %+v", p, action)
								}
							}
						}
						if (values["LATE"] > 0) != (values["LNOTE"] > 0) {
							t.Fatalf("后续片段脚注提前出现: page%d col%d %v", p, col, values)
						}
						if values["H"] > 0 && values["HNOTE"] != 1 {
							t.Fatal("重复表头脚注没有在栏内去重")
						}
						lateNotes += values["LNOTE"]
						headers += values["H"]
					}
				}
				continued := 0
				for value, count := range seen {
					if strings.Contains(value, "(continued)") {
						continued += count
					}
				}
				if lateNotes != 1 || headers < 3 || continued != headers-1 {
					t.Fatalf("续排未覆盖足够片段或续表题: notes=%d headers=%d texts=%v", lateNotes, headers, seen)
				}
			})
		}
	}
}

func TestBalanceHonorsExplicitBoundariesAndSectionPolicy(t *testing.T) {
	o := smallOptions()
	o.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
	o.Theme.Body.Size, o.Theme.Body.LineHeight, o.Theme.Body.SpaceAfter = 10, 1.2, 0
	d := flow.New(o)
	d.AddParagraph(paginationLines("A", 10))
	d.AddPageBreak()
	d.AddParagraph(paginationLines("B", 10))
	d.AddSection(flow.Section{}) // 继承平衡设置；前节仅平衡 B 所在末页。
	d.AddParagraph(paginationLines("C", 10))
	d.AddSection(flow.Section{Columns: &flow.Columns{Count: 2, Gap: 8}})
	d.AddParagraph(paginationLines("D", 10))
	doc := checkPaginationDocument(t, d)
	for prefix, want := range map[string][][]int{
		"A": {{10, 0}, {0, 0}, {0, 0}, {0, 0}}, "B": {{0, 0}, {5, 5}, {0, 0}, {0, 0}},
		"C": {{0, 0}, {0, 0}, {5, 5}, {0, 0}}, "D": {{0, 0}, {0, 0}, {0, 0}, {10, 0}},
	} {
		if got := paginationCounts(doc, prefix, 2); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s 边界改变: %v", prefix, got)
		}
	}
	d = flow.New(o)
	d.AddParagraph(paginationLines("L", 8))
	d.AddColumnBreak()
	d.AddParagraph(paginationLines("R", 2))
	doc = checkPaginationDocument(t, d)
	if got := paginationCounts(doc, "L", 2); !reflect.DeepEqual(got, [][]int{{8, 0}}) {
		t.Fatalf("显式换栏被重排: %v", got)
	}
	if got := paginationCounts(doc, "R", 2); !reflect.DeepEqual(got, [][]int{{0, 2}}) {
		t.Fatalf("换栏后内容改变: %v", got)
	}
}

func TestBalanceKeepsWholeBlocksChainsAndFinalDestinations(t *testing.T) {
	o := smallOptions()
	o.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
	o.GenerateOutlines = true
	o.Theme.Body.Size, o.Theme.Body.LineHeight, o.Theme.Body.SpaceAfter = 10, 1.2, 0
	source := &countedFontSource{data: goregular.TTF}
	o.Fonts[0].Data = nil
	o.Fonts[0].Source = source
	o.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE"}}
	d := flow.New(o)
	d.AddParagraph(paginationLines("P", 5))
	d.Blocks = append(d.Blocks, flow.Block{ID: "h", Kind: "heading", Level: 6, Text: "Heading"})
	d.Blocks = append(d.Blocks, flow.Block{ID: "body", KeepWithNext: true, Runs: []flow.Run{{Text: "Body"}, {Footnote: "note"}}})
	d.Blocks = append(d.Blocks, flow.Block{ID: "box", Kind: "fixed", Fixed: &flow.Fixed{Width: 30, Height: 12, Items: []creator.Item{fixedRect(0, 0, 30, 12)}}})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "See "}, {TitleRef: "h"}, {Text: " page "}, {PageRef: "body"}}})
	doc, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source.opens != 1 {
		t.Fatalf("试排或导航重复读取字体: %d", source.opens)
	}
	if len(doc.Pages) != 1 {
		t.Fatal("平衡改变物理页数")
	}
	var left float64
	for _, bookmark := range doc.Bookmarks {
		if bookmark.Goto.Left == nil || bookmark.Goto.Type != "XYZ" || bookmark.Goto.Page != 0 {
			t.Fatalf("跳转丢失: %+v", bookmark)
		}
		if left == 0 {
			left = *bookmark.Goto.Left
		} else if *bookmark.Goto.Left != left {
			t.Fatal("标题、正文和固定区域跟随链被拆开")
		}
	}
	if left != 54 {
		t.Fatalf("跟随链没有重排到右栏: %g", left)
	}
	if dest := doc.Outlines[0].Actions[0].Goto; dest == nil || dest.Left == nil || *dest.Left != left {
		t.Fatal("大纲未更新栏目的地")
	}
	for _, text := range modelTexts(doc) {
		for _, a := range text.Actions {
			if a.Goto != nil && (a.Goto.Left == nil || *a.Goto.Left != 54) {
				t.Fatalf("正文引用或脚注双向跳转没有更新: %+v", a.Goto)
			}
		}
	}
}

func TestBalanceKeepsTogetherAndUnsplitTableRows(t *testing.T) {
	o := smallOptions()
	o.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
	o.Theme.Body.Size, o.Theme.Body.LineHeight, o.Theme.Body.SpaceAfter = 10, 1.2, 0
	d := flow.New(o)
	d.AddParagraph(paginationLines("P", 5))
	d.Blocks = append(d.Blocks, flow.Block{ID: "whole", Text: paginationLines("K", 6), KeepTogether: true})
	d.AddTable(flow.Table{Rows: [][]string{{"Small row"}}})
	doc := checkPaginationDocument(t, d)
	if got := paginationCounts(doc, "K", 2); !reflect.DeepEqual(got, [][]int{{0, 6}}) {
		t.Fatalf("整段同栏约束改变: %v", got)
	}
	if got := doc.Bookmarks[0].Goto; got.Left == nil || *got.Left != 54 {
		t.Fatalf("整体块目的地没有重排: %+v", got)
	}
}

func TestSplitRowsKeepsMergedHeadersAndEmptyRowsVisible(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{SplitRows: true, HeaderRows: 2, Grid: [][]flow.TableCell{
		{{Text: "H", RowSpan: 2}, {Text: "H1"}}, {{Text: "H2"}},
		{{Text: paginationLines("R", 25), ColSpan: 2}},
	}})
	doc := checkPaginationDocument(t, d)
	for _, page := range doc.Pages {
		for _, header := range []string{"H", "H1", "H2"} {
			if strings.Count(pageText(page), header) == 0 {
				t.Fatal("续页丢失跨行表头")
			}
		}
	}
	d = flow.New(smallOptions())
	d.AddTable(flow.Table{SplitRows: true, Rows: [][]string{{""}}, CellStyle: &flow.CellStyle{Padding: &flow.Margins{}}})
	doc = checkPaginationDocument(t, d)
	paths := 0
	for _, item := range doc.Pages[0].Items {
		if path, ok := item.(creator.Path); ok {
			paths++
			if path.Height <= 0 || path.Width <= 0 {
				t.Fatal("空行边框没有可见高度")
			}
		}
	}
	if paths == 0 {
		t.Fatal("零内边距空行没有绘制边框")
	}
	d = flow.New(smallOptions())
	d.AddTable(flow.Table{SplitRows: true, Header: []string{"Header"}})
	checkPaginationDocument(t, d)
}

func TestBalanceTOCReferencesAndDifferentSectionColumnsConverge(t *testing.T) {
	o := smallOptions()
	o.PageSize = creator.PageSize{Width: 150, Height: 190}
	o.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
	o.GenerateOutlines = true
	d := flow.New(o)
	d.AddTOC(flow.TOC{Title: "Contents", MaxLevel: 1})
	d.AddPageBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "first", Kind: "heading", Level: 1, Text: "First"})
	d.AddParagraph(paginationLines("A", 18))
	d.AddSection(flow.Section{Columns: &flow.Columns{Count: 3, Gap: 6, Balance: true}})
	d.Blocks = append(d.Blocks, flow.Block{ID: "second", Kind: "heading", Level: 1, Text: "Second"})
	d.AddParagraph(paginationLines("B", 15))
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "See "}, {TitleRef: "first"}, {Text: " p"}, {PageRef: "first"}}})
	doc, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 3 || len(doc.Outlines) != 2 || len(doc.Bookmarks) != 2 {
		t.Fatalf("目录与分节重排未稳定: pages=%d outlines=%d bookmarks=%d", len(doc.Pages), len(doc.Outlines), len(doc.Bookmarks))
	}
	for i, bookmark := range doc.Bookmarks {
		if bookmark.Goto.Page != i+1 || bookmark.Goto.Left == nil || bookmark.Goto.Type != "XYZ" {
			t.Fatalf("目录目的地错误: %+v", bookmark)
		}
	}
	links := 0
	for _, text := range modelTexts(doc) {
		for _, action := range text.Actions {
			if action.Goto != nil {
				links++
				if action.Goto.Page < 1 || action.Goto.Page > 2 || action.Goto.Left == nil {
					t.Fatalf("目录或引用目的地错误: %+v", action.Goto)
				}
			}
		}
	}
	if links < 4 {
		t.Fatalf("目录及内部引用丢失: %d", links)
	}
}

func TestBalanceSpreadsRemainderAcrossAllColumns(t *testing.T) {
	for _, columns := range []int{3, 8} {
		t.Run(fmt.Sprint(columns), func(t *testing.T) {
			o := smallOptions()
			o.Columns = flow.Columns{Count: columns, Gap: 8, Balance: true}
			o.Theme.Body.Size, o.Theme.Body.LineHeight, o.Theme.Body.SpaceAfter = 10, 1.2, 0
			if columns == 8 {
				o.Theme.Body.Size = 3
			}
			d := flow.New(o)
			d.AddParagraph(paginationLines("L", 10))
			doc := checkPaginationDocument(t, d)
			counts := paginationCounts(doc, "L", columns)
			if len(counts) != 1 {
				t.Fatalf("平衡新增页面: %v", counts)
			}
			low, high, total := 10, 0, 0
			for _, n := range counts[0] {
				low = min(low, n)
				high = max(high, n)
				total += n
			}
			if low == 0 || high-low > 1 || total != 10 {
				t.Fatalf("余数没有分散到全部栏: %v", counts)
			}
		})
	}
}
