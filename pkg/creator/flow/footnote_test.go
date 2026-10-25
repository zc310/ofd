package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func TestFootnotesStayWithReferenceLinesReserveSpaceAndLinkBothWays(t *testing.T) {
	opts := smallOptions()
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: "NOTE-SOURCE This longer note wraps into several lines and occupies space at the bottom of the same page as its reference."}}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	d.Blocks = append(d.Blocks, flow.Block{ID: "reference", Runs: []flow.Run{{Text: "Referenced sentence"}, {Footnote: "source"}}})
	d.AddParagraph("Following paragraph")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 || model.Bookmarks[0].Goto.Page != 1 {
		t.Fatalf("脚注未将引用行一起移到下一页: pages=%d bookmarks=%+v", len(model.Pages), model.Bookmarks)
	}
	for pageIndex, page := range model.Pages {
		text := pageText(page)
		if strings.Contains(text, "NOTE-SOURCE") != strings.Contains(text, "Referenced sentence") {
			t.Fatalf("脚注和引用不在同页: %q", text)
		}
		var separator *creator.Path
		for _, item := range page.Items {
			if p, ok := item.(creator.Path); ok && math.Abs(p.Height-0.15) < 0.001 {
				copy := p
				separator = &copy
			}
		}
		if pageIndex != 1 {
			if separator != nil {
				t.Fatal("无脚注页出现分隔线")
			}
			continue
		}
		if separator == nil {
			t.Fatal("缺少脚注分隔线")
		}
		forward, backward := 0, 0
		for _, item := range page.Items {
			v, ok := item.(creator.Text)
			if !ok {
				continue
			}
			if v.Y < 10-0.001 || v.Y+v.Height > 80.001 {
				t.Fatalf("脚注或正文越出版心: %+v", v)
			}
			if v.Y < separator.Y && v.Y+v.Height > separator.Y {
				t.Fatalf("正文与脚注重叠: %+v", v)
			}
			if strings.TrimSpace(textValue(v)) != "[1]" {
				continue
			}
			if len(v.Actions) != 1 || v.Actions[0].Goto == nil || v.Actions[0].Goto.Top == nil || v.Actions[0].Goto.Page != pageIndex {
				t.Fatalf("脚注链接无效: %+v", v)
			}
			if v.Y < separator.Y {
				forward++
				if *v.Actions[0].Goto.Top <= separator.Y {
					t.Fatal("标记未指向页底脚注")
				}
			} else {
				backward++
				if *v.Actions[0].Goto.Top >= separator.Y {
					t.Fatal("脚注标记未返回引用处")
				}
			}
		}
		if forward != 1 || backward != 1 {
			t.Fatalf("双向链接数量错误: %d / %d", forward, backward)
		}
	}
	if d.Options.Footnotes[0].Text != opts.Footnotes[0].Text || d.Blocks[1].Runs[1].Text != "" {
		t.Fatal("脚注排版修改了输入")
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("脚注写出不确定")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "footnotes.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("脚注严格校验失败: %+v", report.Issues)
	}
}

func TestFootnoteNumbersFollowFirstUseAndDeduplicateOnlyWithinPage(t *testing.T) {
	opts := smallOptions()
	opts.Footnotes = []flow.Footnote{{ID: "second", Text: "NOTE-SECOND"}, {ID: "first", Text: "NOTE-FIRST"}, {ID: "unused", Text: "UNUSED"}}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "First "}, {Footnote: "first"}, {Text: " again "}, {Footnote: "first"}, {Text: " then "}, {Footnote: "second"}}}}
	d.AddPageBreak()
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Repeated next page "}, {Footnote: "first"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 {
		t.Fatalf("页数错误: %d", len(model.Pages))
	}
	for i, page := range model.Pages {
		text := pageText(page)
		if strings.Count(text, "NOTE-FIRST") != 1 || strings.Contains(text, "UNUSED") {
			t.Fatalf("页内脚注未去重: %q", text)
		}
		if i == 0 && (strings.Count(text, "[1]") != 3 || strings.Count(text, "[2]") != 2 || strings.Count(text, "NOTE-SECOND") != 1) {
			t.Fatalf("首次引用编号错误: %q", text)
		}
		if i == 1 && (strings.Count(text, "[1]") != 2 || strings.Contains(text, "[2]")) {
			t.Fatalf("跨页引用编号改变: %q", text)
		}
	}
}

func TestMultipleNotesOnOneLineAndWidowsDoNotLeaveUnusedFootnotes(t *testing.T) {
	opts := smallOptions()
	opts.Footnotes = []flow.Footnote{{ID: "a", Text: "NOTE-A"}, {ID: "b", Text: "NOTE-B"}}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 6))
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Line one\nLine two\nLine three"}, {Footnote: "a"}, {Footnote: "b"}, {Text: "\nLine four\nLine five"}}, Orphans: 2, Widows: 2})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range model.Pages {
		text := pageText(page)
		for _, test := range []struct{ marker, note string }{{"[1]", "NOTE-A"}, {"[2]", "NOTE-B"}} {
			if strings.Contains(text, test.note) && strings.Count(text, test.marker) != 2 {
				t.Fatalf("孤行调整后留下无引用的脚注: %q", text)
			}
		}
	}
}

func TestTwoDigitFootnoteMarkersStayWhole(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Height = 230
	for i := 0; i < 12; i++ {
		opts.Footnotes = append(opts.Footnotes, flow.Footnote{ID: fmt.Sprintf("n%d", i), Text: fmt.Sprintf("Note number %d", i+1)})
	}
	d := flow.New(opts)
	for i := 0; i < 12; i++ {
		d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Reference"}, {Footnote: fmt.Sprintf("n%d", i)}}, Style: &flow.TextStyle{Set: flow.StyleSpaceAfter}})
	}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && strings.TrimSpace(textValue(text)) == "[12]" {
				count++
				if len(text.Actions) == 0 {
					t.Fatal("两位数标记失去跳转")
				}
			}
		}
	}
	if count != 2 {
		t.Fatalf("两位数脚注标记被拆分或丢失: %d", count)
	}
}

func TestHeadingFollowsFootnoteReferenceAndFollowingFixedBlockRespectsNotes(t *testing.T) {
	opts := smallOptions()
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: "NOTE-SOURCE Explains this paragraph and takes several lines of page space."}}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 6))
	d.Blocks = append(d.Blocks, flow.Block{ID: "heading", Kind: "heading", Level: 2, Text: "Heading"}, flow.Block{ID: "body", Runs: []flow.Run{{Text: "Referenced body"}, {Footnote: "source"}}})
	d.AddFixed(flow.Fixed{Height: 40, Items: []creator.Item{fixedRect(0, 0, 40, 40)}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if model.Bookmarks[0].Goto.Page != 1 || model.Bookmarks[1].Goto.Page != 1 {
		t.Fatalf("标题未跟随带脚注的正文: %+v", model.Bookmarks)
	}
	for _, page := range model.Pages {
		separator := 100.0
		for _, item := range page.Items {
			if p, ok := item.(creator.Path); ok && p.Height == 0.15 {
				separator = p.Y
			}
		}
		for _, item := range page.Items {
			if block, ok := item.(creator.PageBlock); ok {
				p := block.Items[0].(creator.Path)
				if p.Y+p.Height > separator {
					t.Fatal("后续固定区域侵入脚注空间")
				}
			}
		}
	}
}

func TestTableFootnotesRepeatWithHeadersAndKeepMergedGroupsTogether(t *testing.T) {
	opts := smallOptions()
	opts.Footnotes = []flow.Footnote{{ID: "header", Text: "NOTE-HEADER"}, {ID: "group", Text: "NOTE-GROUP Explains a merged pair of rows."}}
	d := flow.New(opts)
	grid := [][]flow.TableCell{{{Runs: []flow.Run{{Text: "Group"}, {Footnote: "header"}}}, {Text: "Result"}}}
	for i := 0; i < 8; i++ {
		runs := []flow.Run{{Text: fmt.Sprintf("G%d", i)}}
		if i == 3 {
			runs = append(runs, flow.Run{Footnote: "group"})
		}
		grid = append(grid, []flow.TableCell{{Runs: runs, RowSpan: 2}, {Text: fmt.Sprintf("A%d", i)}}, []flow.TableCell{{Text: fmt.Sprintf("B%d", i)}})
	}
	d.AddTable(flow.Table{HeaderRows: 1, Grid: grid, Caption: "Results", RepeatCaption: true})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 3 {
		t.Fatalf("未覆盖多个续页: %d", len(model.Pages))
	}
	for _, page := range model.Pages {
		text := pageText(page)
		if strings.Count(text, "NOTE-HEADER") != 1 || strings.Count(text, "[1]") != 2 {
			t.Fatalf("续页表头缺少同页脚注: %q", text)
		}
		if strings.Contains(text, "NOTE-GROUP") && (!strings.Contains(text, "G3") || !strings.Contains(text, "A3") || !strings.Contains(text, "B3")) {
			t.Fatalf("合并行组的脚注被拆开: %q", text)
		}
		separator := 100.0
		for _, item := range page.Items {
			if p, ok := item.(creator.Path); ok && p.Height == 0.15 {
				separator = p.Y
			}
		}
		for _, item := range page.Items {
			if p, ok := item.(creator.Path); ok && p.Height != 0.15 && p.Y+p.Height > separator {
				t.Fatalf("表格侵入脚注区域: %+v", p)
			}
		}
	}
}

func TestFootnotesUseSectionGeometryNamedStylesAndFinalPageReferences(t *testing.T) {
	opts := smallOptions()
	fontSource := &countedFontSource{data: opts.Fonts[0].Data}
	opts.Fonts[0].Source = fontSource
	opts.Styles = []flow.NamedStyle{{Name: "note", Style: flow.TextStyle{Size: 8, Set: flow.StyleSpaceAfter}}}
	opts.FootnoteStyle = &flow.TextStyle{BasedOn: "note"}
	opts.Footnotes = []flow.Footnote{{ID: "a", Runs: []flow.Run{{Text: "NOTE-REF Appendix page "}, {PageRef: "appendix"}, {Text: " with "}, {Text: "bold", Bold: true}}}}
	d := flow.New(opts)
	d.AddTOC()
	d.AddPageBreak()
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Portrait "}, {Footnote: "a"}}})
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 140, Height: 90}})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Landscape "}, {Footnote: "a"}}})
	d.Blocks = append(d.Blocks, flow.Block{ID: "appendix", Kind: "heading", Level: 1, Text: "Appendix", PageBreakBefore: true})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fontSource.opens != 1 {
		t.Fatalf("脚注多轮排版重复加载字体: %d", fontSource.opens)
	}
	appendix := 0
	for _, bookmark := range model.Bookmarks {
		if bookmark.Name == "appendix" {
			appendix = bookmark.Goto.Page
		}
	}
	seen, bold := 0, false
	for _, page := range model.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			if strings.HasPrefix(textValue(text), "NOTE-REF") {
				seen++
				if math.Abs(text.Size-8*25.4/72) > 0.001 {
					t.Fatal("脚注命名样式丢失")
				}
			}
			if textValue(text) == "bold" {
				bold = text.Font == "Flow-Bold"
			}
			if strings.TrimSpace(textValue(text)) == fmt.Sprint(appendix+1) && len(text.Actions) > 0 && text.Actions[0].Goto != nil && text.Actions[0].Goto.Page != appendix {
				t.Fatal("脚注中的页码引用不指向最终页")
			}
		}
	}
	if seen != 2 || !bold {
		t.Fatalf("跨节脚注或富文本缺失: seen=%d bold=%v", seen, bold)
	}
	if d.Options.FootnoteStyle.BasedOn != "note" || !reflect.DeepEqual(d.Options.Footnotes[0].Runs[1], flow.Run{PageRef: "appendix"}) {
		t.Fatal("脚注解析修改了输入")
	}
}

func TestFootnotesRejectInvalidDefinitionsReferencesAndImpossibleLayout(t *testing.T) {
	for _, test := range []struct {
		name  string
		notes []flow.Footnote
		block flow.Block
		want  string
	}{
		{"duplicate", []flow.Footnote{{ID: "a", Text: "A"}, {ID: "a", Text: "B"}}, flow.Block{Text: "Body"}, "重复"},
		{"empty id", []flow.Footnote{{Text: "A"}}, flow.Block{Text: "Body"}, "不能为空"},
		{"empty text", []flow.Footnote{{ID: "a"}}, flow.Block{Text: "Body"}, "内容不能为空"},
		{"mixed definition", []flow.Footnote{{ID: "a", Text: "A", Runs: []flow.Run{{Text: "B"}}}}, flow.Block{Text: "Body"}, "互斥"},
		{"nested", []flow.Footnote{{ID: "a", Runs: []flow.Run{{Footnote: "a"}}}}, flow.Block{Text: "Body"}, "不能嵌套"},
		{"bad control", []flow.Footnote{{ID: "a", Text: "bad\x00"}}, flow.Block{Text: "Body"}, "控制字符"},
		{"missing", nil, flow.Block{Runs: []flow.Run{{Footnote: "missing"}}}, "不存在"},
		{"mixed run", []flow.Footnote{{ID: "a", Text: "A"}}, flow.Block{Runs: []flow.Run{{Text: "bad", Footnote: "a"}}}, "互斥"},
		{"heading", []flow.Footnote{{ID: "a", Text: "A"}}, flow.Block{Kind: "heading", Level: 1, Runs: []flow.Run{{Footnote: "a"}}}, "标题不支持"},
		{"too long", []flow.Footnote{{ID: "a", Text: strings.Repeat("line\n", 40)}}, flow.Block{Runs: []flow.Run{{Footnote: "a"}}}, "整页版心"},
		{"unbreakable", []flow.Footnote{{ID: "a", Text: strings.Repeat("W", 100)}}, flow.Block{Runs: []flow.Run{{Footnote: "a"}}}, "超过行宽"},
		{"empty runs", []flow.Footnote{{ID: "a", Runs: []flow.Run{{Text: " "}}}}, flow.Block{Runs: []flow.Run{{Footnote: "a"}}}, "可显示"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := smallOptions()
			opts.Footnotes = test.notes
			d := flow.New(opts)
			d.Blocks = []flow.Block{test.block}
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误=%v，期望 %q", err, test.want)
			}
		})
	}
}
