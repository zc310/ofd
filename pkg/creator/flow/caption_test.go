package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func TestCaptionNumbersAreIndependentAndReferencesUseFinalDestinations(t *testing.T) {
	opts := smallOptions()
	opts.ReferencePageNumbers = "section"
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "See "}, {CaptionRef: "data"}, {Text: " on page "}, {PageRef: "data"}, {Text: " and "}, {CaptionRef: "picture"}}})
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	d.AddImage(flow.Image{Data: data.Bytes(), Width: 8, Caption: "Unnumbered"})
	d.AddImage(flow.Image{Data: data.Bytes(), Width: 8, Caption: "First", Numbered: true})
	d.AddTable(flow.Table{Rows: [][]string{{"Unnumbered"}}, Caption: "Plain table"})
	d.AddTable(flow.Table{Rows: [][]string{{"First"}}, Numbered: true})
	start := 4
	d.AddSection(flow.Section{StartPageNumber: &start, PageNumberStyle: "roman"})
	d.Blocks = append(d.Blocks, flow.Block{ID: "data", Kind: "table", Table: &flow.Table{Caption: "Results", Numbered: true, CaptionPrefix: "Exhibit", Columns: []flow.TableColumn{{}, {}}, Grid: [][]flow.TableCell{{{Runs: []flow.Run{{CaptionRef: "picture"}}}, {Runs: []flow.Run{{CaptionRef: "data"}}}}}}})
	d.Blocks = append(d.Blocks, flow.Block{ID: "picture", Kind: "image", Image: &flow.Image{Data: data.Bytes(), Width: 8, Caption: "Second", Numbered: true}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bookmarks := map[string]creator.GotoAction{}
	for _, b := range model.Bookmarks {
		bookmarks[b.Name] = b.Goto
	}
	refs, pageRef := 0, false
	allText := ""
	for _, page := range model.Pages {
		allText += pageText(page)
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok || len(text.Actions) == 0 {
				continue
			}
			value, target := strings.TrimSpace(textValue(text)), ""
			switch value {
			case "Exhibit 2", "iv":
				target = "data"
			case "Figure 2":
				target = "picture"
			}
			if target == "" {
				continue
			}
			destination := bookmarks[target]
			if !reflect.DeepEqual(text.Actions[0].Goto, &destination) {
				t.Fatalf("编号引用跳转错误: %+v", text)
			}
			if value == "iv" {
				pageRef = true
			} else {
				refs++
			}
		}
	}
	for _, want := range []string{"Figure 1  First", "Figure 2  Second", "Table 1", "Exhibit 2  Results"} {
		if !strings.Contains(allText, want) {
			t.Fatalf("题注缺少 %q: %q", want, allText)
		}
	}
	if refs != 4 || !pageRef {
		t.Fatalf("编号与页码引用缺失: refs=%d pageRef=%v text=%q bookmarks=%+v", refs, pageRef, allText, bookmarks)
	}
	if d.Blocks[len(d.Blocks)-2].Table.Caption != "Results" || d.Blocks[0].Runs[1].Text != "" {
		t.Fatal("编号回填修改了输入")
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("重复创建的图表编号不稳定")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "captions.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("严格校验失败: %+v", report.Issues)
	}
}

func TestContinuationCaptionStaysWithRepeatedHeaderAndMergedGroup(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	grid := [][]flow.TableCell{{{Text: "Group"}, {Text: "Result"}}}
	for i := 0; i < 10; i++ {
		grid = append(grid, []flow.TableCell{{Text: fmt.Sprintf("G%d", i), RowSpan: 2}, {Text: fmt.Sprintf("A%d", i)}}, []flow.TableCell{{Text: fmt.Sprintf("B%d", i)}})
	}
	d.Blocks = append(d.Blocks, flow.Block{ID: "table", Kind: "table", Table: &flow.Table{Caption: "Data", Numbered: true, RepeatCaption: true, ContinuationSuffix: "(cont.)", HeaderRows: 1, Grid: grid}})
	d.AddParagraph("After table")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tablePages := 0
	for pageIndex, page := range model.Pages {
		text := pageText(page)
		if !strings.Contains(text, "Table 1  Data") {
			continue
		}
		tablePages++
		if !strings.Contains(text, "Group\n") || !strings.Contains(text, "Result\n") || !strings.Contains(text, "G") || strings.Count(text, "Table 1  Data") != 1 {
			t.Fatalf("表题、表头或行组分离: %q", text)
		}
		if (tablePages > 1) != strings.Contains(text, "(cont.)") {
			t.Fatalf("续表标记错误: %q", text)
		}
		for i := 0; i < 10; i++ {
			if strings.Contains(text, fmt.Sprintf("G%d\n", i)) && (!strings.Contains(text, fmt.Sprintf("A%d\n", i)) || !strings.Contains(text, fmt.Sprintf("B%d\n", i))) {
				t.Fatalf("续表拆开合并行组: %q", text)
			}
		}
		if tablePages == 1 {
			if pageIndex != model.Bookmarks[0].Goto.Page || *model.Bookmarks[0].Goto.Top < 10 || pageIndex == 0 {
				t.Fatalf("表题锚点未随整组移动: %+v", model.Bookmarks[0])
			}
		}
		for _, item := range page.Items {
			if value, ok := item.(creator.Text); ok && (value.Y < 9.999 || value.Y+value.Height > 80.001) {
				t.Fatalf("续表文字越界: %+v", value)
			}
		}
	}
	if tablePages < 3 {
		t.Fatalf("未覆盖多个续页: %d", tablePages)
	}
}

func TestCaptionValidationRejectsAmbiguousOrMissingReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		blocks []flow.Block
		want   string
	}{
		{"missing", []flow.Block{{Runs: []flow.Run{{CaptionRef: "missing"}}}}, "带编号"},
		{"unnumbered", []flow.Block{{Runs: []flow.Run{{CaptionRef: "data"}}}, {ID: "data", Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, Caption: "Plain"}}}, "带编号"},
		{"mixed", []flow.Block{{Runs: []flow.Run{{Text: "bad", CaptionRef: "data"}}}, {ID: "data", Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, Numbered: true}}}, "互斥"},
		{"mixed refs", []flow.Block{{Runs: []flow.Run{{PageRef: "data", CaptionRef: "data"}}}, {ID: "data", Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, Numbered: true}}}, "互斥"},
		{"heading", []flow.Block{{Kind: "heading", Level: 1, Runs: []flow.Run{{CaptionRef: "data"}}}, {ID: "data", Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, Numbered: true}}}, "标题不支持"},
		{"prefix", []flow.Block{{Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, CaptionPrefix: "Table"}}}, "需要 numbered"},
		{"no repeat caption", []flow.Block{{Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, RepeatCaption: true}}}, "需要非空题注"},
		{"bad prefix", []flow.Block{{Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, Numbered: true, CaptionPrefix: "bad\x00"}}}, "控制字符"},
		{"oversized caption", []flow.Block{{Kind: "table", Table: &flow.Table{Rows: [][]string{{"Data"}}, Caption: strings.Repeat("Title\n", 30)}}}, "整页版心"},
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
}

func TestSingleGroupTableDoesNotReserveUnusedContinuationCaption(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddTable(flow.Table{Caption: "Only table", RepeatCaption: true, ContinuationSuffix: strings.Repeat("Continuation\n", 20), Rows: [][]string{{"Only row"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 1 || strings.Contains(pageText(model.Pages[0]), "Continuation") {
		t.Fatal("没有续页时不应预留或绘制续表题")
	}
}

func TestNumberedImageKeepsCaptionWithinPageAndAnchorsImageTop(t *testing.T) {
	d := flow.New(smallOptions())
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	d.Blocks = append(d.Blocks, flow.Block{ID: "image", Kind: "image", Image: &flow.Image{Data: data.Bytes(), Width: 25, Numbered: true, Caption: "A longer caption that wraps over more than one line"}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	target := model.Bookmarks[0].Goto
	if target.Page == 0 {
		t.Fatal("图片与编号图注没有整块分页")
	}
	for _, item := range model.Pages[target.Page].Items {
		switch v := item.(type) {
		case creator.Image:
			if math.Abs(v.Y-*target.Top) > 0.001 {
				t.Fatal("图片锚点偏移")
			}
		case creator.Text:
			if v.Y+v.Height > 80.001 {
				t.Fatal("图注越界")
			}
		}
	}
}
