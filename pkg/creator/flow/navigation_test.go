package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

type countedFontSource struct {
	data  []byte
	opens int
}

func (s *countedFontSource) Open() (io.ReadCloser, error) {
	s.opens++
	return io.NopCloser(bytes.NewReader(s.data)), nil
}
func (s *countedFontSource) Size() int64 { return int64(len(s.data)) }

func TestTOCAndPageReferencesMatchFinalPages(t *testing.T) {
	opts := smallOptions()
	opts.GenerateOutlines = true
	source := &countedFontSource{data: opts.Fonts[0].Data}
	opts.Fonts[0].Source = source
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{ID: "cover", Kind: "heading", Level: 1, Text: "Cover"})
	d.AddPageBreak()
	d.AddTOC(flow.TOC{Title: "Contents", MaxLevel: 2})
	d.AddPageBreak()
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Appendix page "}, {PageRef: "appendix"}, {Text: "; "}, {Text: "go to table", Target: "data"}}})
	for i := 0; i < 12; i++ {
		d.Blocks = append(d.Blocks, flow.Block{ID: fmt.Sprintf("chapter-%d", i), Kind: "heading", Level: 2, Text: fmt.Sprintf("Chapter %02d long heading title", i), PageBreakBefore: true})
		d.AddParagraph("Body text.")
	}
	d.Blocks = append(d.Blocks, flow.Block{ID: "data", Kind: "table", Table: &flow.Table{Grid: [][]flow.TableCell{{{Runs: []flow.Run{{Text: "Back to cover", Target: "cover"}}}, {Runs: []flow.Run{{PageRef: "appendix"}}}}}}})
	d.Blocks = append(d.Blocks, flow.Block{ID: "appendix", Kind: "heading", Level: 1, Text: "Appendix", PageBreakBefore: true})
	d.AddParagraph("Final section.")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source.opens != 1 {
		t.Fatalf("多轮排版重复读取字体来源: %d", source.opens)
	}
	bookmarks := map[string]creator.GotoAction{}
	for _, b := range model.Bookmarks {
		bookmarks[b.Name] = b.Goto
	}
	if bookmarks["chapter-0"].Page < 3 {
		t.Fatalf("跨页目录未改变正文页码: %+v", bookmarks["chapter-0"])
	}
	if bookmarks["appendix"].Page < 10 {
		t.Fatal("未覆盖两位数页码")
	}
	tocTitles, tocNumbers, refs := 0, 0, 0
	for pageIndex, page := range model.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			value := textValue(text)
			if text.X < 10-0.001 || text.X+text.Width > 90.001 || text.Y < 10-0.001 || text.Y+text.Height > 80.001 {
				t.Fatalf("目录或引用文字越界: %+v", text)
			}
			if len(text.Actions) == 0 || text.Actions[0].Goto == nil {
				continue
			}
			dest := text.Actions[0].Goto
			if dest.Page < 0 || dest.Page >= len(model.Pages) || dest.Top == nil {
				t.Fatalf("跳转失效: %+v", dest)
			}
			if value == fmt.Sprint(dest.Page+1) {
				if pageIndex < bookmarks["chapter-0"].Page-1 {
					tocNumbers++
				} else {
					refs++
				}
			}
			if strings.Contains(value, "Chapter") {
				tocTitles++
			}
		}
	}
	if tocTitles < 12 || tocNumbers < 14 || refs < 2 {
		t.Fatalf("目录或引用缺失: titles=%d numbers=%d refs=%d", tocTitles, tocNumbers, refs)
	}
	if d.Blocks[2].TOC.Title != "Contents" || d.Blocks[4].Runs[1].Text != "" {
		t.Fatal("多轮排版修改了输入模型")
	}
	var first, second bytes.Buffer
	if err := d.Write(context.Background(), &first, creator.CreateOptions{Deterministic: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), &second, creator.CreateOptions{Deterministic: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("目录生成未保持确定性")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "toc.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("目录文件严格校验失败: %+v", report.Issues)
	}
}

func TestNavigationRejectsInvalidIDsAndReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		blocks []flow.Block
		want   string
	}{
		{"duplicate", []flow.Block{{ID: "x", Text: "a"}, {ID: "x", Text: "b"}}, "重复"},
		{"reserved", []flow.Block{{ID: "__flow_x", Text: "a"}}, "保留前缀"},
		{"whitespace", []flow.Block{{ID: "bad id", Text: "a"}}, "空白"},
		{"missing", []flow.Block{{Runs: []flow.Run{{Text: "link", Target: "missing"}}}}, "不存在"},
		{"missing page", []flow.Block{{Runs: []flow.Run{{PageRef: "missing"}}}}, "不存在"},
		{"mixed destinations", []flow.Block{{ID: "x", Runs: []flow.Run{{Text: "link", Target: "x", Href: "https://example.com"}}}}, "不能同时"},
		{"mixed page text", []flow.Block{{ID: "x", Runs: []flow.Run{{Text: "bad", PageRef: "x"}}}}, "互斥"},
		{"empty target", []flow.Block{{ID: "x"}}, "可定位"},
		{"break target", []flow.Block{{ID: "x", Kind: "page_break"}}, "不能设置"},
		{"no headings", []flow.Block{{Kind: "toc"}}, "没有非空标题"},
		{"invalid toc", []flow.Block{{Kind: "toc", TOC: &flow.TOC{MaxLevel: 7}}}, "1 到 6"},
		{"toc contents", []flow.Block{{Kind: "toc", Text: "bad"}}, "不能携带"},
		{"wrong toc block", []flow.Block{{TOC: &flow.TOC{}}}, "type 为 toc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := flow.New()
			d.Blocks = test.blocks
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误 = %v，期望包含 %q", err, test.want)
			}
		})
	}
}

func TestIDAnchorsFollowKeepRulesAndInternalLinksStaySeparate(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddParagraph(strings.Repeat("prefix\n", 7))
	d.Blocks = append(d.Blocks, flow.Block{ID: "moved", Kind: "heading", Level: 2, Text: "Moved"})
	d.AddParagraph("first\nsecond\nthird")
	d.Blocks = append(d.Blocks, flow.Block{ID: "links", Runs: []flow.Run{{Text: "one", Target: "moved"}, {Text: "two", Target: "links"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var target creator.GotoAction
	for _, b := range model.Bookmarks {
		if b.Name == "moved" {
			target = b.Goto
		}
	}
	if target.Page != 1 {
		t.Fatalf("锚点仍指向标题换页前的位置: %+v", target)
	}
	var destinations []*creator.GotoAction
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && len(text.Actions) > 0 {
				destinations = append(destinations, text.Actions[0].Goto)
			}
		}
	}
	if len(destinations) != 2 || reflect.DeepEqual(destinations[0], destinations[1]) {
		t.Fatalf("相邻不同内部链接被合并: %+v", destinations)
	}
}

func TestTOCLevelFilterAndUntitledDirectory(t *testing.T) {
	opts := smallOptions()
	opts.GenerateOutlines = true
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{ID: "contents", Kind: "toc", TOC: &flow.TOC{MaxLevel: 1}, PageBreakAfter: true})
	d.AddHeading(1, "Visible")
	d.AddHeading(2, "Excluded")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := pageText(model.Pages[0])
	if strings.Contains(text, "Contents") || strings.Contains(text, "Excluded") || !strings.Contains(text, "Visible") {
		t.Fatalf("目录标题或级别过滤错误: %q", text)
	}
	if len(model.Outlines) != 1 || model.Outlines[0].Title != "Visible" {
		t.Fatalf("目录进入了大纲: %+v", model.Outlines)
	}
	if len(model.Bookmarks) != 3 {
		t.Fatalf("匿名标题或无标题目录没有锚点: %+v", model.Bookmarks)
	}
}

func TestImageIDUsesPositionAfterWholeBlockPagination(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddParagraph(strings.Repeat("prefix\n", 8))
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	d.Blocks = append(d.Blocks, flow.Block{ID: "figure", Kind: "image", Image: &flow.Image{Data: data.Bytes(), Width: 35, Caption: "Figure"}})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{PageRef: "figure"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var target creator.GotoAction
	for _, b := range model.Bookmarks {
		if b.Name == "figure" {
			target = b.Goto
		}
	}
	if target.Page != 1 || target.Top == nil {
		t.Fatalf("图片锚点没有跟随整块换页: %+v", target)
	}
	imageFound, referenceFound := false, false
	for _, item := range model.Pages[1].Items {
		if img, ok := item.(creator.Image); ok {
			imageFound = true
			if math.Abs(img.Y-*target.Top) > 0.001 {
				t.Fatalf("图片锚点偏移: %+v / %+v", img, target)
			}
		}
		if text, ok := item.(creator.Text); ok && textValue(text) == "2" {
			referenceFound = true
		}
	}
	if !imageFound || !referenceFound {
		t.Fatal("图片或图片页码引用丢失")
	}
}

func TestContentIDCannotOverwriteMetadataBookmark(t *testing.T) {
	d := flow.New()
	d.Metadata.Bookmarks = []creator.Bookmark{{Name: "same", Goto: creator.GotoAction{Page: 0, Type: "Fit"}}}
	d.Blocks = []flow.Block{{ID: "same", Text: "text"}}
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "Metadata.Bookmarks") {
		t.Fatalf("未拒绝覆盖已有书签: %v", err)
	}
}

func TestTOCExpansionPreservesSourceDiagnosticIndex(t *testing.T) {
	d := flow.New()
	d.AddTOC()
	d.AddHeading(1, "First")
	d.AddHeading(1, "Second")
	d.AddParagraph("text", flow.TextStyle{Font: "missing"})
	_, err := d.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "blocks[3]") {
		t.Fatalf("目录展开后错误未指向原始块: %v", err)
	}
}
