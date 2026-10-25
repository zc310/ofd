package flow_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func TestColumnsFlowTopToBottomThenLeftToRightAcrossPages(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	opts.Theme.Body.Size, opts.Theme.Body.LineHeight, opts.Theme.Body.SpaceAfter = 10, 1.2, 0
	d := flow.New(opts)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("Line-%02d", i))
	}
	d.AddParagraph(strings.Join(lines, "\n"))
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 {
		t.Fatalf("未按两栏分页: pages=%d", len(model.Pages))
	}
	var found []string
	for pageIndex, page := range model.Pages {
		if page.Area.ContentBox.Width != 80 {
			t.Fatalf("物理版心被替换为单栏: %+v", page.Area.ContentBox)
		}
		columns := map[int]int{}
		previousColumn, previousY := 0, 0.0
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			column, left := 0, 10.0
			if text.X >= 54 {
				column, left = 1, 54
			}
			if text.X < left-0.001 || text.X+text.Width > left+36.001 || text.Y < 9.999 || text.Y+text.Height > 80.001 {
				t.Fatalf("分栏文字越界: %+v", text)
			}
			if column < previousColumn || column == previousColumn && text.Y < previousY {
				t.Fatal("文字未按栏序输出")
			}
			previousColumn, previousY = column, text.Y
			columns[column]++
			found = append(found, textValue(text))
		}
		if pageIndex == 0 && (columns[0] == 0 || columns[1] == 0) {
			t.Fatalf("首页未使用两栏: %v", columns)
		}
	}
	if !reflect.DeepEqual(found, lines) {
		t.Fatalf("换栏丢失或重复正文: %v", found)
	}
}

func TestColumnBreakPageBreakAndDestinationsUsePhysicalPages(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	opts.GenerateOutlines, opts.PageNumbers = true, true
	opts.Header = "Header"
	d := flow.New(opts)
	d.AddColumnBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "left", Kind: "heading", Level: 2, Text: "Left"})
	d.AddColumnBreak()
	d.AddColumnBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "right", Kind: "heading", Level: 2, Text: "Right"})
	d.AddPageBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "next", Text: "Next page"}, flow.Block{Runs: []flow.Run{{Text: "Right page "}, {PageRef: "right"}}})
	d.AddColumnBreak()
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 || len(model.Bookmarks) != 3 || len(model.Outlines) != 2 {
		t.Fatalf("显式换栏或换页产生空白内容: pages=%d bookmarks=%d outlines=%d", len(model.Pages), len(model.Bookmarks), len(model.Outlines))
	}
	for i, want := range []struct {
		page int
		left float64
	}{{0, 10}, {0, 54}, {1, 10}} {
		gotoAction := model.Bookmarks[i].Goto
		if gotoAction.Page != want.page || gotoAction.Type != "XYZ" || gotoAction.Left == nil || *gotoAction.Left != want.left {
			t.Fatalf("导航未定位实际栏: %+v", gotoAction)
		}
	}
	if destination := model.Outlines[1].Actions[0].Goto; destination == nil || destination.Left == nil || *destination.Left != 54 || destination.Type != "XYZ" {
		t.Fatalf("大纲未保留右栏位置: %+v", destination)
	}
	for _, page := range model.Pages {
		header, footer := 0, 0
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				if textValue(text) == "Header" {
					header++
					if text.X != 10 {
						t.Fatal("页眉跟随右栏偏移")
					}
				}
				if strings.Contains(textValue(text), "/ 2") {
					footer++
					if text.X < 35 || text.X > 55 {
						t.Fatal("页码没有按整页居中")
					}
				}
			}
		}
		if header != 1 || footer != 1 {
			t.Fatalf("页眉页脚按栏重复: %d / %d", header, footer)
		}
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("分栏写出不确定")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "columns.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("分栏严格校验失败: %+v", report.Issues)
	}
}

func TestSectionsInheritColumnsAndCanRestoreSingleColumn(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	d := flow.New(opts)
	d.AddParagraph("Initial")
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 140, Height: 90}})
	d.AddParagraph("Wide-left")
	d.AddColumnBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "wide-right", Text: "Wide-right"})
	d.AddSection(flow.Section{Columns: &flow.Columns{Count: 3, Gap: 3}})
	d.AddParagraph("First")
	d.AddColumnBreak()
	d.AddParagraph("Second")
	d.AddColumnBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "third", Text: "Third"})
	d.AddSection(flow.Section{Columns: &flow.Columns{Count: 1}})
	d.Blocks = append(d.Blocks, flow.Block{ID: "single", Text: "Full width"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 4 {
		t.Fatalf("分节未从新物理页开始: %d", len(model.Pages))
	}
	for i, want := range []struct {
		page int
		left float64
	}{{1, 74}, {2, 92}} {
		got := model.Bookmarks[i].Goto
		if got.Page != want.page || got.Left == nil || *got.Left != want.left {
			t.Fatalf("分栏继承或三栏位置错误: %+v", got)
		}
	}
	if got := model.Bookmarks[2].Goto; got.Page != 3 || got.Type != "FitH" || got.Left != nil {
		t.Fatalf("未恢复单栏目的地: %+v", got)
	}
	if model.Pages[3].Area.ContentBox.Width != 120 {
		t.Fatal("单栏恢复没有保留纸张尺寸")
	}
}

func TestColumnTOCUsesFinalPageAndHorizontalHeadingDestinations(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	fontSource := &countedFontSource{data: opts.Fonts[0].Data}
	opts.Fonts[0].Source = fontSource
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{Kind: "toc", TOC: &flow.TOC{MaxLevel: 1}, Style: &flow.TextStyle{Size: 9, Set: flow.StyleSpaceAfter}, PageBreakAfter: true})
	for i := 0; i < 10; i++ {
		if i > 0 {
			d.AddColumnBreak()
		}
		d.Blocks = append(d.Blocks, flow.Block{ID: fmt.Sprintf("h%d", i), Kind: "heading", Level: 1, Text: fmt.Sprintf("H%02d", i)})
	}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fontSource.opens != 1 {
		t.Fatalf("分栏目录迭代重复加载字体: %d", fontSource.opens)
	}
	targets := map[string]creator.GotoAction{}
	for _, bookmark := range model.Bookmarks {
		targets[bookmark.Name] = bookmark.Goto
	}
	links := 0
	for _, item := range model.Pages[0].Items {
		text, ok := item.(creator.Text)
		if !ok || len(text.Actions) == 0 {
			continue
		}
		value := strings.TrimSpace(textValue(text))
		for i := 0; i < 10; i++ {
			if value == fmt.Sprintf("H%02d", i) {
				want := targets[fmt.Sprintf("h%d", i)]
				if !reflect.DeepEqual(text.Actions[0].Goto, &want) {
					t.Fatalf("目录未保留实际栏位置: %+v / %+v", text.Actions[0].Goto, want)
				}
				links++
			}
		}
	}
	if links != 10 {
		t.Fatalf("分栏目录条目丢失: %d", links)
	}
}

func TestHeadingFollowsBodyIntoNextColumn(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 8))
	d.Blocks = append(d.Blocks, flow.Block{ID: "heading", Kind: "heading", Level: 2, Text: "Title"}, flow.Block{ID: "body", Text: "Body text"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 1 || len(model.Bookmarks) != 2 {
		t.Fatal("标题跟随产生了多余物理页")
	}
	for _, bookmark := range model.Bookmarks {
		if bookmark.Goto.Left == nil || *bookmark.Goto.Left != 54 {
			t.Fatalf("标题与正文未一起进入右栏: %+v", bookmark)
		}
	}
}

func TestFootnotesReserveSpaceAndLinkWithinTheirOwnColumns(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: "NOTE-SOURCE For this column."}}
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Left"}, {Footnote: "source"}}})
	d.AddColumnBreak()
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Right"}, {Footnote: "source"}}})
	d.AddSection(flow.Section{Columns: &flow.Columns{Count: 1}})
	d.AddParagraph("Final single column")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	markers, separators := 0, 0
	for _, item := range model.Pages[0].Items {
		if p, ok := item.(creator.Path); ok && p.Height == 0.15 {
			separators++
			if p.Width != 36 || p.X != 10 && p.X != 54 {
				t.Fatalf("脚注分隔线越栏: %+v", p)
			}
		}
		if text, ok := item.(creator.Text); ok {
			left := 10.0
			if text.X >= 54 {
				left = 54
			}
			if text.X+text.Width > left+36.001 || text.Y+text.Height > 80.001 {
				t.Fatalf("脚注或正文越栏: %+v", text)
			}
			if strings.TrimSpace(textValue(text)) != "[1]" {
				continue
			}
			markers++
			gotoAction := text.Actions[0].Goto
			if gotoAction.Page != 0 || gotoAction.Type != "XYZ" || gotoAction.Left == nil || *gotoAction.Left != left {
				t.Fatalf("脚注跳转到了另一栏: %+v", gotoAction)
			}
		}
	}
	if markers != 4 || separators != 2 {
		t.Fatalf("脚注未按栏分别显示: markers=%d separators=%d", markers, separators)
	}
}

func TestTablesRepeatHeadersCaptionsAndFootnotesOnColumnContinuation(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Width = 150
	opts.Columns = flow.Columns{Count: 2, Gap: 10}
	opts.Footnotes = []flow.Footnote{{ID: "header", Text: "NOTE-HEADER"}}
	d := flow.New(opts)
	grid := [][]flow.TableCell{{{Text: "Group"}, {Runs: []flow.Run{{Text: "Value"}, {Footnote: "header"}}}}}
	for i := 0; i < 8; i++ {
		grid = append(grid, []flow.TableCell{{Text: fmt.Sprintf("G%d", i), RowSpan: 2}, {Text: fmt.Sprintf("A%d", i)}}, []flow.TableCell{{Text: fmt.Sprintf("B%d", i)}})
	}
	d.AddTable(flow.Table{Caption: "Results", RepeatCaption: true, HeaderRows: 1, Grid: grid})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fragments := 0
	for _, page := range model.Pages {
		for _, left := range []float64{10, 80} {
			values := ""
			separator := 100.0
			for _, item := range page.Items {
				if text, ok := item.(creator.Text); ok && text.X >= left-0.001 && text.X < left+60 {
					values += textValue(text) + "\n"
					if text.X+text.Width > left+60.001 {
						t.Fatalf("表格文字越栏: %+v", text)
					}
				}
				if p, ok := item.(creator.Path); ok && p.X == left && p.Height == 0.15 {
					separator = p.Y
				}
			}
			if values == "" {
				continue
			}
			fragments++
			if strings.Count(values, "Group\n") != 1 || strings.Count(values, "NOTE-HEADER\n") != 1 || !strings.Contains(values, "Results") {
				t.Fatalf("换栏丢失表头、题注或脚注: %q", values)
			}
			for i := 0; i < 8; i++ {
				if strings.Contains(values, fmt.Sprintf("G%d\n", i)) && (!strings.Contains(values, fmt.Sprintf("A%d\n", i)) || !strings.Contains(values, fmt.Sprintf("B%d\n", i))) {
					t.Fatalf("合并行组跨栏拆分: %q", values)
				}
			}
			for _, item := range page.Items {
				if p, ok := item.(creator.Path); ok && p.X >= left && p.X < left+60 && p.Height != 0.15 && p.Y+p.Height > separator {
					t.Fatal("表格侵入栏底脚注")
				}
			}
		}
	}
	if fragments < 3 {
		t.Fatalf("未覆盖换栏与换页: %d", fragments)
	}
}

func TestImagesAndFixedRegionsUseCurrentColumnWidth(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	d := flow.New(opts)
	d.AddFixed(flow.Fixed{Height: 10, Items: []creator.Item{fixedRect(0, 0, 36, 10)}})
	d.AddColumnBreak()
	d.AddFixed(flow.Fixed{Width: 20, Height: 10, Align: "right", Items: []creator.Item{fixedRect(0, 0, 20, 10)}})
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgDr8AAAGjATO977V+AAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	d.AddImage(flow.Image{Width: 100, Data: data})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	paths := []float64{}
	imageFound := false
	for _, item := range model.Pages[0].Items {
		switch v := item.(type) {
		case creator.PageBlock:
			paths = append(paths, v.Items[0].(creator.Path).X)
		case creator.Image:
			imageFound = true
			if v.X != 54 || math.Abs(v.Width-36) > 0.001 {
				t.Fatalf("图片没有适配当前栏: %+v", v)
			}
		}
	}
	if !reflect.DeepEqual(paths, []float64{10, 70}) || !imageFound {
		t.Fatalf("固定块位置或图片丢失: %v / %v", paths, imageFound)
	}
}

func TestColumnsAndColumnBreaksRejectInvalidConfiguration(t *testing.T) {
	for _, columns := range []flow.Columns{{Count: -1}, {Count: 9}, {Count: 2, Gap: -1}, {Count: 2, Gap: math.NaN()}, {Count: 2, Gap: math.Inf(1)}, {Count: 2, Gap: 80}} {
		opts := smallOptions()
		opts.Columns = columns
		d := flow.New(opts)
		d.AddParagraph("Invalid")
		if _, err := d.Build(context.Background()); err == nil {
			t.Fatalf("未拒绝无效分栏: %+v", columns)
		}
	}
	for _, block := range []flow.Block{{ID: "break", Kind: "column_break"}, {Kind: "column_break", Text: "Invalid"}, {Kind: "section", Section: &flow.Section{Columns: &flow.Columns{Count: 2, Gap: 80}}}} {
		d := flow.New(smallOptions())
		d.Blocks = []flow.Block{block}
		if _, err := d.Build(context.Background()); err == nil {
			t.Fatalf("未拒绝无效换栏或分节: %+v", block)
		}
	}
}
