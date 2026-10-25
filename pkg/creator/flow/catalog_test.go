package flow_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func catalogDestinationKey(action creator.GotoAction) string {
	left, top := 0.0, 0.0
	if action.Left != nil {
		left = *action.Left
	}
	if action.Top != nil {
		top = *action.Top
	}
	return fmt.Sprintf("%d:%s:%.6f:%.6f", action.Page, action.Type, left, top)
}

func TestFigureAndTableListsCollectNumberedBlocksAndResolveFinalPages(t *testing.T) {
	opts := smallOptions()
	opts.GenerateOutlines = true
	fontSource := &countedFontSource{data: opts.Fonts[0].Data}
	opts.Fonts[0].Source = fontSource
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{ID: "figures", Kind: "toc", TOC: &flow.TOC{Title: "Figures", Source: "figures"}, PageBreakAfter: true},
		flow.Block{ID: "tables", Kind: "toc", TOC: &flow.TOC{Title: "Tables", Source: "tables"}, PageBreakAfter: true},
		flow.Block{ID: "body", Kind: "heading", Level: 1, Text: "Body"})
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgDr8AAAGjATO977V+AAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("figure-%d", i)
		if i == 1 {
			id = ""
		}
		if i == 2 {
			d.Blocks = append(d.Blocks, flow.Block{ID: id, Kind: "image", Image: &flow.Image{Data: imageData, Width: 8, Caption: fmt.Sprintf("Detailed diagram %02d explaining the measured result", i), Numbered: true}})
			continue
		}
		d.Blocks = append(d.Blocks, flow.Block{ID: id, Kind: "fixed", Fixed: &flow.Fixed{Height: 10, Items: []creator.Item{fixedRect(0, 0, 20, 10)}, Caption: fmt.Sprintf("Detailed diagram %02d explaining the measured result", i), Numbered: true}})
	}
	d.AddFixed(flow.Fixed{Height: 5, Items: []creator.Item{fixedRect(0, 0, 10, 5)}, Caption: "Not listed"})
	rows := make([][]string, 15)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(i), "Result"}
	}
	d.Blocks = append(d.Blocks, flow.Block{ID: "table", Kind: "table", Table: &flow.Table{Header: []string{"ID", "Value"}, Rows: rows, Caption: "Measurements", Numbered: true, RepeatCaption: true}})
	d.AddTable(flow.Table{Rows: [][]string{{"Final result"}}, Caption: "Summary", Numbered: true})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fontSource.opens != 1 {
		t.Fatalf("图表目录分页回填重复加载字体: %d", fontSource.opens)
	}
	bookmarks := map[string]creator.GotoAction{}
	for _, b := range model.Bookmarks {
		bookmarks[b.Name] = b.Goto
	}
	if bookmarks["body"].Page < 4 {
		t.Fatalf("长图目录没有参与分页: %+v", bookmarks["body"])
	}
	if len(model.Outlines) != 1 || model.Outlines[0].Title != "Body" {
		t.Fatalf("图表目录标题进入了大纲: %+v", model.Outlines)
	}
	pageRefs := map[string]int{}
	for pageIndex, page := range model.Pages {
		if pageIndex >= bookmarks["body"].Page {
			continue
		}
		if strings.Contains(pageText(page), "Not listed") {
			t.Fatal("未编号内容进入图目录")
		}
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok || len(text.Actions) == 0 || text.Actions[0].Goto == nil {
				continue
			}
			destination := *text.Actions[0].Goto
			if strings.TrimSpace(textValue(text)) == fmt.Sprint(destination.Page+1) {
				pageRefs[catalogDestinationKey(destination)]++
			}
		}
	}
	anonymous := 0
	for name, destination := range bookmarks {
		if name == "figures" || name == "tables" || name == "body" {
			continue
		}
		if strings.HasPrefix(name, "__flow_") {
			anonymous++
		}
		if pageRefs[catalogDestinationKey(destination)] != 1 {
			t.Fatalf("图表目标 %q 缺少唯一目录页码，或续表重复入目录: %v", name, pageRefs)
		}
	}
	if len(pageRefs) != 14 || anonymous != 2 {
		t.Fatalf("图表目录条目或匿名书签错误: entries=%d anonymous=%d", len(pageRefs), anonymous)
	}
	if d.Blocks[4].ID != "" || d.Blocks[3].Fixed.Caption != "Detailed diagram 00 explaining the measured result" {
		t.Fatal("自动目录修改了原始内容或 ID")
	}
	var out bytes.Buffer
	if err := creator.CreateWithOptions(*model, &out, creator.CreateOptions{Deterministic: true}); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(out.Bytes()), "catalogs.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("图表目录严格校验失败: %+v", report.Issues)
	}
}

func TestTitleReferencesReadFullTitlesAndRefreshAfterEdits(t *testing.T) {
	d := flow.New(smallOptions())
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgDr8AAAGjATO977V+AAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"chapter", "image", "diagram", "table"} {
		d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{TitleRef: id}}})
	}
	d.Blocks = append(d.Blocks,
		flow.Block{ID: "chapter", Kind: "heading", Level: 2, Runs: []flow.Run{{Text: "First ", Bold: true}, {Text: "chapter\n title"}}},
		flow.Block{ID: "image", Kind: "image", Image: &flow.Image{Data: data, Width: 8, Caption: "Overview", Numbered: true}},
		flow.Block{ID: "diagram", Kind: "fixed", Fixed: &flow.Fixed{Height: 5, Items: []creator.Item{fixedRect(0, 0, 10, 5)}, Caption: "Measured\nresults", Numbered: true, CaptionPrefix: "Chart"}},
		flow.Block{ID: "table", Kind: "table", Table: &flow.Table{Rows: [][]string{{"Value"}}, Caption: "Plain table"}})
	for pass := 0; pass < 2; pass++ {
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byDestination := map[string][]string{}
		for _, page := range model.Pages {
			for _, item := range page.Items {
				if text, ok := item.(creator.Text); ok && len(text.Actions) > 0 && text.Actions[0].Goto != nil {
					if text.Font != "Flow-Regular" {
						t.Fatalf("标题引用意外继承了目标字体: %+v", text)
					}
					key := catalogDestinationKey(*text.Actions[0].Goto)
					byDestination[key] = append(byDestination[key], textValue(text))
				}
			}
		}
		want := map[string]string{"chapter": "First chapter title", "image": "Figure 1 Overview", "diagram": "Chart 2 Measured results", "table": "Plain table"}
		if pass == 1 {
			want["chapter"], want["image"] = "Updated chapter", "Figure 1 Revised view"
		}
		for _, bookmark := range model.Bookmarks {
			got := strings.Join(strings.Fields(strings.Join(byDestination[catalogDestinationKey(bookmark.Goto)], " ")), " ")
			if got != want[bookmark.Name] {
				t.Fatalf("标题引用 %q=%q，期望 %q", bookmark.Name, got, want[bookmark.Name])
			}
		}
		if pass == 0 {
			d.Blocks[4].Runs = []flow.Run{{Text: "Updated chapter"}}
			d.Blocks[5].Image.Caption = "Revised view"
		}
	}
	if d.Blocks[0].Runs[0].Text != "" || d.Blocks[5].Image.Caption != "Revised view" {
		t.Fatal("标题回填修改了输入")
	}
}

func TestCaptionCatalogsUseSectionLabelsAndColumnDestinations(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	opts.ReferencePageNumbers = "section"
	opts.Footnotes = []flow.Footnote{{ID: "note", Runs: []flow.Run{{Text: "See "}, {TitleRef: "right"}}}}
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{Kind: "toc", TOC: &flow.TOC{Source: "figures"}, Style: &flow.TextStyle{Size: 9}, PageBreakAfter: true})
	start, prefix := 5, "A-"
	d.AddSection(flow.Section{StartPageNumber: &start, PageNumberStyle: "ROMAN", PageNumberPrefix: &prefix})
	d.Blocks = append(d.Blocks, flow.Block{ID: "left", Kind: "fixed", Fixed: &flow.Fixed{Height: 10, Items: []creator.Item{fixedRect(0, 0, 20, 10)}, Caption: "Left", Numbered: true}})
	d.AddColumnBreak()
	d.Blocks = append(d.Blocks, flow.Block{ID: "right", Kind: "fixed", Fixed: &flow.Fixed{Height: 10, Items: []creator.Item{fixedRect(0, 0, 20, 10)}, Caption: "Right", Numbered: true}})
	d.AddSection(flow.Section{Columns: &flow.Columns{Count: 1}})
	d.AddTable(flow.Table{Grid: [][]flow.TableCell{{{Runs: []flow.Run{{TitleRef: "left"}}}}}})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Details"}, {Footnote: "note"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bookmarks := map[string]creator.GotoAction{}
	for _, bookmark := range model.Bookmarks {
		bookmarks[bookmark.Name] = bookmark.Goto
	}
	if right := bookmarks["right"]; right.Type != "XYZ" || right.Left == nil || *right.Left != 54 || right.Page != 1 {
		t.Fatalf("图目录目标未保留栏位置: %+v", right)
	}
	labels := 0
	for _, item := range model.Pages[0].Items {
		text, ok := item.(creator.Text)
		if !ok || len(text.Actions) == 0 {
			continue
		}
		if strings.TrimSpace(textValue(text)) == "A-V" {
			labels++
			if text.Actions[0].Goto.Page != 1 {
				t.Fatal("图目录显示编号改变了物理跳转")
			}
		}
	}
	if labels != 2 {
		t.Fatalf("图目录未回填节内编号: %d", labels)
	}
	leftRef, rightRef := false, false
	for _, item := range model.Pages[len(model.Pages)-1].Items {
		text, ok := item.(creator.Text)
		if !ok || len(text.Actions) == 0 || text.Actions[0].Goto == nil {
			continue
		}
		if strings.TrimSpace(textValue(text)) == "Figure 1 Left" {
			leftRef = reflect.DeepEqual(text.Actions[0].Goto, pointerToGoto(bookmarks["left"]))
		}
		if strings.TrimSpace(textValue(text)) == "Figure 2 Right" {
			rightRef = reflect.DeepEqual(text.Actions[0].Goto, pointerToGoto(bookmarks["right"]))
		}
	}
	if !leftRef || !rightRef {
		t.Fatalf("单元格或脚注中的标题引用缺失: left=%v right=%v", leftRef, rightRef)
	}
}

func pointerToGoto(value creator.GotoAction) *creator.GotoAction { return &value }

func TestCatalogHelpersKeepDefaultsAndAllowUntitledLists(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddFigureList()
	d.AddTableList("")
	if d.Blocks[0].TOC.Source != "figures" || d.Blocks[0].TOC.Title != "Figures" || d.Blocks[1].TOC.Source != "tables" || d.Blocks[1].TOC.Title != "" {
		t.Fatal("图表目录便捷方法默认值错误")
	}
}

func TestCatalogsAndTitleReferencesRejectInvalidTargetsAndOptions(t *testing.T) {
	figure := flow.Block{ID: "figure", Kind: "fixed", Fixed: &flow.Fixed{Height: 5, Items: []creator.Item{fixedRect(0, 0, 10, 5)}, Caption: "Chart", Numbered: true}}
	for _, test := range []struct {
		name   string
		blocks []flow.Block
		want   string
	}{
		{"bad source", []flow.Block{{Kind: "toc", TOC: &flow.TOC{Source: "images"}}}, "source"},
		{"figure levels", []flow.Block{{Kind: "toc", TOC: &flow.TOC{Source: "figures", MaxLevel: 2}}, figure}, "max_level 仅用于"},
		{"empty figures", []flow.Block{{Kind: "toc", TOC: &flow.TOC{Source: "figures"}}}, "没有带编号"},
		{"empty tables", []flow.Block{{Kind: "toc", TOC: &flow.TOC{Source: "tables"}}, figure}, "没有带编号"},
		{"missing title", []flow.Block{{Runs: []flow.Run{{TitleRef: "missing"}}}}, "非空章节标题"},
		{"paragraph target", []flow.Block{{ID: "body", Text: "Body"}, {Runs: []flow.Run{{TitleRef: "body"}}}}, "非空章节标题"},
		{"empty heading", []flow.Block{{ID: "empty", Kind: "heading", Level: 1, Text: " "}, {Runs: []flow.Run{{TitleRef: "empty"}}}}, "非空章节标题"},
		{"mixed text", []flow.Block{figure, {Runs: []flow.Run{{Text: "bad", TitleRef: "figure"}}}}, "互斥"},
		{"mixed reference", []flow.Block{figure, {Runs: []flow.Run{{PageRef: "figure", TitleRef: "figure"}}}}, "互斥"},
		{"heading reference", []flow.Block{figure, {Kind: "heading", Level: 1, Runs: []flow.Run{{TitleRef: "figure"}}}}, "标题不支持"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := flow.New(smallOptions())
			d.Blocks = test.blocks
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误=%v，期望 %q", err, test.want)
			}
		})
	}
	d := flow.New(smallOptions())
	d.AddFigureList()
	d.Blocks = append(d.Blocks, figure)
	d.AddParagraph("Text", flow.TextStyle{Font: "missing"})
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "blocks[2]") {
		t.Fatalf("图目录展开丢失原始块下标: %v", err)
	}
}
