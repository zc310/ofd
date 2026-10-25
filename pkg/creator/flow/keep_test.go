package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func bookmarkPages(model *creator.Document) map[string]int {
	pages := make(map[string]int)
	for _, bookmark := range model.Bookmarks {
		pages[bookmark.Name] = bookmark.Goto.Page
	}
	return pages
}

// 标题、主送机关、正文开头应作为连续跟随链，不能逐对换页后把标题留在前页。
func TestOfficialTitleRecipientAndBodyKeepChainMovesTogether(t *testing.T) {
	for _, prefixLines := range []int{15, 16} {
		t.Run(fmt.Sprint(prefixLines), func(t *testing.T) {
			d := flow.New(officialTestOptions())
			d.AddParagraph(strings.Repeat("Prefix\n", prefixLines))
			d.Blocks = append(d.Blocks, flow.Block{ID: "title", Kind: "heading", Level: 1, Text: "Notice"}, flow.Block{ID: "recipient", Kind: "recipient", Text: "All departments:"}, flow.Block{ID: "body", Text: "Body line one\nBody line two"})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			pages := bookmarkPages(model)
			if len(model.Pages) != 2 || pages["title"] != 1 || pages["recipient"] != 1 || pages["body"] != 1 {
				t.Fatalf("连续跟随链被拆分: pages=%v total=%d", pages, len(model.Pages))
			}
			var first, second bytes.Buffer
			for _, out := range []*bytes.Buffer{&first, &second} {
				if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(first.Bytes(), second.Bytes()) {
				t.Fatal("跟随链输出不确定")
			}
			v, err := validator.New(validator.WithMode(validator.ModeStrict))
			if err != nil {
				t.Fatal(err)
			}
			report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "keep.ofd")
			if report.HasErrors() || report.HasWarnings() {
				t.Fatalf("跟随链输出严格校验失败: %+v", report.Issues)
			}
		})
	}
}

func TestKeepChainReservesEntireFixedRegionAndCaption(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 5))
	d.Blocks = append(d.Blocks, flow.Block{ID: "lead", Text: "Lead", KeepWithNext: true}, flow.Block{ID: "middle", Text: "Middle", KeepWithNext: true}, flow.Block{ID: "fixed", Kind: "fixed", Fixed: &flow.Fixed{Width: 40, Height: 30, Caption: "Caption", Items: []creator.Item{fixedRect(0, 0, 40, 30)}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	if pages["lead"] != 1 || pages["middle"] != 1 || pages["fixed"] != 1 || !strings.Contains(pageText(model.Pages[1]), "Caption") {
		t.Fatalf("固定图元区域及题注未与前导链一起移动: %v", pages)
	}
}

// 多个显式跟随段落可跨换栏，导航应指向同一栏的实际位置。
func TestKeepChainMovesThroughAllFollowingBlocksAndColumns(t *testing.T) {
	for _, columns := range []bool{false, true} {
		t.Run(fmt.Sprint(columns), func(t *testing.T) {
			opts := smallOptions()
			opts.Theme.Body.SpaceAfter = 0
			if columns {
				opts.Columns = flow.Columns{Count: 2, Gap: 6}
			}
			d := flow.New(opts)
			d.AddParagraph(strings.Repeat("Prefix\n", 6))
			for i := 0; i < 4; i++ {
				d.Blocks = append(d.Blocks, flow.Block{ID: fmt.Sprintf("keep%d", i), Text: fmt.Sprintf("Keep%d", i), KeepWithNext: true})
			}
			d.Blocks = append(d.Blocks, flow.Block{ID: "body", Text: "Body one\nBody two"})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			pages := bookmarkPages(model)
			wantPage := 1
			if columns {
				wantPage = 0
			}
			for _, id := range []string{"keep0", "keep1", "keep2", "keep3", "body"} {
				if pages[id] != wantPage {
					t.Fatalf("跟随块没有整体移动: %v", pages)
				}
			}
			if columns {
				for _, bookmark := range model.Bookmarks {
					if bookmark.Goto.Left == nil || *bookmark.Goto.Left != 53 {
						t.Fatalf("跟随链未进入右栏: %+v", bookmark)
					}
				}
			}
		})
	}
}

// 跟随链尾部的新脚注必须在搬移前一起试算，前一页不得遗留未引用的注释。
func TestKeepChainIncludesFootnotesFromItsLastBlock(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	opts.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE line one\nline two\nline three"}}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 5))
	d.Blocks = append(d.Blocks, flow.Block{ID: "lead", Text: "Lead", KeepWithNext: true}, flow.Block{ID: "middle", Text: "Middle", KeepWithNext: true}, flow.Block{ID: "body", Runs: []flow.Run{{Text: "Body one"}, {Footnote: "note"}, {Text: "\nBody two"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	if pages["lead"] != 1 || pages["middle"] != 1 || pages["body"] != 1 || strings.Contains(pageText(model.Pages[0]), "NOTE") || !strings.Contains(pageText(model.Pages[1]), "NOTE") {
		t.Fatalf("跟随链没有连同脚注一起换页: %v", pages)
	}
}

func TestKeepChainIncludesWholeTailWhenKeptTogether(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 5))
	d.Blocks = append(d.Blocks, flow.Block{ID: "lead", Text: "Lead", KeepWithNext: true}, flow.Block{ID: "middle", Text: "Middle", KeepWithNext: true}, flow.Block{ID: "tail", Text: "One\nTwo\nThree\nFour", KeepTogether: true})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	if pages["lead"] != 1 || pages["middle"] != 1 || pages["tail"] != 1 || !strings.Contains(pageText(model.Pages[1]), "One\nTwo\nThree\nFour") {
		t.Fatalf("整块同页的尾部把前导块留在上一页: %v", pages)
	}
}

// 三行短段落不能按默认 2+2 孤行约束拆分，前导链需为它预留完整三行。
func TestKeepChainIncludesShortTailRequiredByWidows(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 6))
	d.Blocks = append(d.Blocks, flow.Block{ID: "lead", Text: "Lead", KeepWithNext: true}, flow.Block{ID: "middle", Text: "Middle", KeepWithNext: true}, flow.Block{ID: "tail", Text: "One\nTwo\nThree"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	if pages["lead"] != 1 || pages["middle"] != 1 || pages["tail"] != 1 {
		t.Fatalf("短段落的孤行约束拆开了跟随链: %v", pages)
	}
}

// 表格尾部只要求题注、完整表头和首个行组与链同页，后面的行仍可跨页。
func TestKeepChainEndsAtTableMinimumWithRepeatedHeadersAndFootnotes(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	opts.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE one\ntwo\nthree"}}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 5))
	d.Blocks = append(d.Blocks, flow.Block{ID: "lead", Text: "Lead", KeepWithNext: true}, flow.Block{ID: "middle", Text: "Middle", KeepWithNext: true})
	grid := [][]flow.TableCell{{{Runs: []flow.Run{{Text: "Header"}, {Footnote: "note"}}}}}
	for i := 0; i < 18; i++ {
		grid = append(grid, []flow.TableCell{{Text: fmt.Sprintf("Row %02d", i)}})
	}
	d.Blocks = append(d.Blocks, flow.Block{ID: "table", Kind: "table", Table: &flow.Table{Caption: "Results", RepeatCaption: true, HeaderRows: 1, Grid: grid}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	if pages["lead"] != 1 || pages["middle"] != 1 || pages["table"] != 1 {
		t.Fatalf("表头及脚注没有与前导链一起换页: %v", pages)
	}
	if len(model.Pages) < 3 || !strings.Contains(pageText(model.Pages[1]), "Row 00") || strings.Contains(pageText(model.Pages[0]), "NOTE") {
		t.Fatal("表格首次出现或后续分页错误")
	}
	for _, page := range model.Pages {
		if strings.Contains(pageText(page), "Row ") && strings.Count(pageText(page), "NOTE") != 1 {
			t.Fatal("续页重复表头没有保留所属页脚注")
		}
	}
}

func TestKeepChainStopsAtExplicitPageColumnAndSectionBoundaries(t *testing.T) {
	for _, boundary := range []string{"before", "after", "page_break", "column_break", "section"} {
		t.Run(boundary, func(t *testing.T) {
			opts := smallOptions()
			opts.Theme.Body.SpaceAfter = 0
			opts.Columns = flow.Columns{Count: 2, Gap: 6}
			d := flow.New(opts)
			d.AddParagraph("Prefix")
			d.Blocks = append(d.Blocks, flow.Block{ID: "lead", Text: "Lead", KeepWithNext: true}, flow.Block{ID: "middle", Text: "Middle", KeepWithNext: true})
			if boundary == "after" {
				d.Blocks[len(d.Blocks)-1].PageBreakAfter = true
			}
			if boundary == "page_break" || boundary == "column_break" {
				d.Blocks = append(d.Blocks, flow.Block{Kind: boundary})
			}
			if boundary == "section" {
				d.AddSection(flow.Section{})
			}
			d.Blocks = append(d.Blocks, flow.Block{ID: "tail", Text: "Body one\nBody two", PageBreakBefore: boundary == "before"})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			pages := bookmarkPages(model)
			if pages["lead"] != 0 || pages["middle"] != 0 {
				t.Fatal("强制边界拉动了之前的跟随块")
			}
			want := 1
			if boundary == "column_break" {
				want = 0
			}
			if pages["tail"] != want {
				t.Fatalf("跟随链忽略了强制边界: %v", pages)
			}
			if boundary == "column_break" {
				for _, bookmark := range model.Bookmarks {
					if bookmark.Name == "tail" && (bookmark.Goto.Left == nil || *bookmark.Goto.Left != 53) {
						t.Fatal("显式换栏未进入右栏")
					}
				}
			}
		})
	}
}

// 整条链超高时必须放宽，仍输出所有内容，不能不停增加空页或要求整条链同页。
func TestOversizedKeepChainRelaxesWithoutBlankPagesOrLostText(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	d := flow.New(opts)
	for i := 0; i < 40; i++ {
		d.Blocks = append(d.Blocks, flow.Block{ID: fmt.Sprintf("keep%d", i), Text: fmt.Sprintf("Item%02d", i), KeepWithNext: true})
	}
	d.AddParagraph("Final one\nFinal two")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 4 || len(model.Pages) > 10 || len(model.Bookmarks) != 40 {
		t.Fatalf("超长链页数或内容丢失: pages=%d bookmarks=%d", len(model.Pages), len(model.Bookmarks))
	}
	var all strings.Builder
	for _, page := range model.Pages {
		text := pageText(page)
		if text == "" {
			t.Fatal("超长链产生空页")
		}
		all.WriteString(text)
	}
	for i := 0; i < 40; i++ {
		if strings.Count(all.String(), fmt.Sprintf("Item%02d", i)) != 1 {
			t.Fatalf("超长链内容 %d 丢失或重复", i)
		}
	}
	if !strings.Contains(all.String(), "Final one\nFinal two") {
		t.Fatal("链尾正文丢失")
	}
}
