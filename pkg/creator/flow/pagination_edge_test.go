package flow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 覆盖先有脚注、表格各列在不同位置引用脚注，以及内边距变化时的片段预留。
func TestSplitRowsMultipleCellNotesDoNotOverlapOrLoseContent(t *testing.T) {
	for _, columns := range []int{1, 2} {
		for _, padding := range []float64{0, 2} {
			for _, balanced := range []bool{false, true} {
				t.Run(fmt.Sprintf("columns%d-padding%g-balance%v", columns, padding, balanced), func(t *testing.T) {
					o := smallOptions()
					o.Columns = flow.Columns{Count: columns, Gap: 8, Balance: balanced}
					o.Footnotes = []flow.Footnote{{ID: "prefix", Text: "PREFIX-NOTE"}, {ID: "late", Text: "LATE-NOTE"}, {ID: "early", Text: "EARLY-NOTE"}}
					d := flow.New(o)
					d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Prefix"}, {Footnote: "prefix"}}})
					d.AddTable(flow.Table{
						SplitRows: true, HeaderRows: 1,
						CellStyle: &flow.CellStyle{Padding: &flow.Margins{Top: padding, Right: padding, Bottom: padding, Left: padding}},
						Grid: [][]flow.TableCell{
							{{Text: "H1"}, {Text: "H2"}},
							{{Runs: []flow.Run{{Text: paginationLines("A", 18)}, {Footnote: "late"}}}, {Runs: []flow.Run{{Footnote: "early"}, {Text: paginationLines("B", 13)}}}},
							{{Text: "End", ColSpan: 2}},
						},
					})
					before, err := json.Marshal(d.Blocks)
					if err != nil {
						t.Fatal(err)
					}
					doc := checkPaginationDocument(t, d)
					after, err := json.Marshal(d.Blocks)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(before, after) {
						t.Fatal("片段试排修改了输入内容或样式")
					}
					seen := map[string]int{}
					for _, text := range modelTexts(doc) {
						seen[textValue(text)]++
					}
					for prefix, count := range map[string]int{"A": 18, "B": 13} {
						for i := 0; i < count; i++ {
							if seen[fmt.Sprintf("%s%02d", prefix, i)] != 1 {
								t.Fatalf("%s 第 %d 行丢失或重复", prefix, i)
							}
						}
					}
					for _, note := range []string{"PREFIX-NOTE", "LATE-NOTE", "EARLY-NOTE"} {
						// 小栏内的注释可能自动换行，按每栏完整文字核对。
						found := 0
						for _, page := range doc.Pages {
							for col := 0; col < columns; col++ {
								var content strings.Builder
								for _, item := range page.Items {
									if text, ok := item.(creator.Text); ok && paginationColumn(text.X, columns) == col {
										content.WriteString(textValue(text))
									}
								}
								found += strings.Count(content.String(), note)
							}
						}
						if found != 1 {
							t.Fatalf("脚注 %q 丢失或重复: %d", note, found)
						}
					}
					for _, page := range doc.Pages {
						for _, item := range page.Items {
							separator, ok := item.(creator.Path)
							if !ok || math.Abs(separator.Height-0.15) > 0.000001 {
								continue
							}
							for _, other := range page.Items {
								text, ok := other.(creator.Text)
								if ok && paginationColumn(text.X, columns) == paginationColumn(separator.X, columns) && text.Y < separator.Y && text.Y+text.Height > separator.Y+0.001 {
									t.Fatalf("正文或表格文字覆盖脚注分隔线: %+v", text)
								}
							}
						}
					}
				})
			}
		}
	}
}

func paginationColumn(x float64, columns int) int {
	return min(columns-1, int((x-10+0.001)/((80.0+8)/float64(columns))))
}

func TestBalancePreservesBlockPageBreaksAndTrailingSectionGeometry(t *testing.T) {
	for _, boundary := range []string{"before", "after"} {
		t.Run(boundary, func(t *testing.T) {
			o := smallOptions()
			o.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
			o.Theme.Body.Size, o.Theme.Body.LineHeight, o.Theme.Body.SpaceAfter = 10, 1.2, 0
			d := flow.New(o)
			d.Blocks = []flow.Block{{Text: paginationLines("A", 10)}, {Text: paginationLines("B", 10)}}
			if boundary == "before" {
				d.Blocks[1].PageBreakBefore = true
			} else {
				d.Blocks[0].PageBreakAfter = true
			}
			// 没有后续内容的分节不能修改已有页面或产生空白尾页。
			d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 200, Height: 250}, Columns: &flow.Columns{Count: 3, Gap: 10}})
			doc := checkPaginationDocument(t, d)
			if got := paginationCounts(doc, "A", 2); !reflect.DeepEqual(got, [][]int{{10, 0}, {0, 0}}) {
				t.Fatalf("分页前内容改变: %v", got)
			}
			if got := paginationCounts(doc, "B", 2); !reflect.DeepEqual(got, [][]int{{0, 0}, {5, 5}}) {
				t.Fatalf("分页后的末页未平衡: %v", got)
			}
			for _, page := range doc.Pages {
				if page.Area.PhysicalBox.Width != 100 || page.Area.PhysicalBox.Height != 90 || page.Area.ContentBox.Width != 80 {
					t.Fatal("结尾分节污染原页面几何")
				}
			}
		})
	}
}

func TestSplitRowsReportsImpossibleHeaderOrFootnoteWithoutEmptyContinuation(t *testing.T) {
	for _, impossible := range []string{"header", "footnote"} {
		t.Run(impossible, func(t *testing.T) {
			o := smallOptions()
			d := flow.New(o)
			table := flow.Table{SplitRows: true, Header: []string{"Header"}, Grid: nil, Rows: [][]string{{"Body"}}}
			if impossible == "header" {
				table.Header[0] = paginationLines("H", 30)
			} else {
				d.Options.Footnotes = []flow.Footnote{{ID: "large", Text: paginationLines("NOTE", 30)}}
				table.Header, table.Rows = nil, nil
				table.Grid = [][]flow.TableCell{{{Runs: []flow.Run{{Text: "Body"}, {Footnote: "large"}}}}}
			}
			d.AddTable(table)
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), "版心") {
				t.Fatalf("无解内容未明确报错: %v", err)
			}
		})
	}
}

// 使用检查预算触发真实的上下文取消，避免依赖机器速度和定时器。
type paginationCancelContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *paginationCancelContext) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestSplitRowsPreservesCancellationDuringLayout(t *testing.T) {
	d := flow.New(smallOptions())
	d.Options.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
	d.AddTable(flow.Table{SplitRows: true, Rows: [][]string{{paginationLines("Cell", 1000)}}})
	before, err := json.Marshal(d.Blocks)
	if err != nil {
		t.Fatal(err)
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &paginationCancelContext{Context: base, cancel: cancel, remaining: 100}
	if doc, err := d.Build(ctx); !errors.Is(err, context.Canceled) || doc != nil {
		t.Fatalf("取消返回了部分文档或丢失原因: doc=%v err=%v", doc != nil, err)
	}
	after, err := json.Marshal(d.Blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("取消后的输入被修改")
	}
}
