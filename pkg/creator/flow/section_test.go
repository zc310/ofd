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

func ptr[T any](value T) *T { return &value }

func TestSectionsRestartNumbersAndUsePageSpecificGeometry(t *testing.T) {
	opts := smallOptions()
	opts.PageNumbers = true
	opts.ReferencePageNumbers = "section"
	opts.GenerateOutlines = true
	d := flow.New(opts)
	d.AddSection(flow.Section{Header: ptr(""), PageNumbers: ptr(false)})
	d.Blocks = append(d.Blocks, flow.Block{ID: "cover", Text: "Cover"})
	d.AddSection(flow.Section{Header: ptr("Contents header"), PageNumbers: ptr(true), StartPageNumber: ptr(1), PageNumberStyle: "roman"})
	d.AddTOC(flow.TOC{Title: "Contents", MaxLevel: 2})
	d.AddSection(flow.Section{Header: ptr("Body header"), FirstPageHeader: ptr("Opening header"), FirstPageFooter: ptr(""), HideFirstPageNumber: true, StartPageNumber: ptr(1), PageNumberStyle: "decimal"})
	d.Blocks = append(d.Blocks, flow.Block{ID: "body", Kind: "heading", Level: 1, Text: "Body"})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Appendix page "}, {PageRef: "appendix"}}})
	d.AddParagraph(strings.Repeat("body line\n", 20))
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 200, Height: 100}, Margins: &flow.Margins{Top: 10, Right: 10, Bottom: 10, Left: 10}, Header: ptr("Wide header")})
	d.Blocks = append(d.Blocks, flow.Block{ID: "wide", Kind: "heading", Level: 1, Text: "Wide table"})
	d.AddTable(flow.Table{Header: []string{"One", "Two", "Three", "Four", "Five"}, Rows: [][]string{{"1", "2", "3", "4", "5"}}, ColumnWidths: []float64{36, 36, 36, 36, 36}})
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 100, Height: 90}, Header: ptr("Appendix header"), StartPageNumber: ptr(1), PageNumberPrefix: ptr("A-")})
	d.Blocks = append(d.Blocks, flow.Block{ID: "appendix", Kind: "heading", Level: 1, Text: "Appendix"})
	d.AddParagraph("Final content")
	d.AddSection(flow.Section{})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bookmarks := map[string]creator.GotoAction{}
	for _, b := range model.Bookmarks {
		bookmarks[b.Name] = b.Goto
	}
	bodyPage, widePage, appendixPage := bookmarks["body"].Page, bookmarks["wide"].Page, bookmarks["appendix"].Page
	if bodyPage < 2 || appendixPage != len(model.Pages)-1 {
		t.Fatalf("分节增加了空页或错页: body=%d appendix=%d total=%d", bodyPage, appendixPage, len(model.Pages))
	}
	if strings.Contains(pageText(model.Pages[0]), " / ") {
		t.Fatal("封面显示了自动页码")
	}
	if !strings.Contains(pageText(model.Pages[1]), fmt.Sprintf("i / %d", len(model.Pages))) {
		t.Fatal("目录没有使用罗马页码")
	}
	if text := pageText(model.Pages[bodyPage]); !strings.Contains(text, "Opening header") || strings.Contains(text, "Body header") || strings.Contains(text, "1 / ") {
		t.Fatalf("正文首页特殊设置错误: %q", text)
	}
	if text := pageText(model.Pages[bodyPage+1]); !strings.Contains(text, "Body header") || !strings.Contains(text, fmt.Sprintf("2 / %d", len(model.Pages))) {
		t.Fatalf("正文续页页码错误: %q", text)
	}
	if text := pageText(model.Pages[appendixPage]); !strings.Contains(text, "A-1 / ") || strings.Contains(text, "Opening header") {
		t.Fatalf("附录编号或首页设置继承错误: %q", text)
	}
	if model.Pages[widePage].Area.PhysicalBox.Width != 200 || model.Pages[widePage].Area.PhysicalBox.Height != 100 {
		t.Fatal("横向节未设置实际页面尺寸")
	}
	if model.Pages[appendixPage].Area.PhysicalBox.Width != 100 {
		t.Fatal("附录未恢复纵向页面")
	}
	referenceFound := false
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				box := page.Area.PhysicalBox
				if text.X < 9.999 || text.X+text.Width > box.Width-9.999 || text.Y < 0 || text.Y+text.Height > box.Height {
					t.Fatalf("混合页面文字越界: %+v / %+v", text, box)
				}
				if textValue(text) == "A-1" && len(text.Actions) > 0 {
					referenceFound = true
					if dest := text.Actions[0].Goto; dest == nil || dest.Page != appendixPage {
						t.Fatalf("逻辑编号改变了物理跳转目标: %+v", text.Actions)
					}
				}
			}
		}
	}
	if !referenceFound {
		t.Fatal("目录或正文没有回填逻辑页码")
	}
	var output bytes.Buffer
	if err := d.Write(context.Background(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(output.Bytes()), "sections.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("分节文档严格校验失败: %+v", report.Issues)
	}
}

func TestSectionsPreserveDefaultPhysicalReferences(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddParagraph("Cover")
	d.AddSection(flow.Section{StartPageNumber: ptr(8), Header: ptr("{page}|{pages}|{physical_page}|{section_page}|{section_pages}")})
	d.Blocks = append(d.Blocks, flow.Block{ID: "body", Runs: []flow.Run{{PageRef: "body"}}})
	d.AddPageBreak()
	d.AddParagraph("Continuation")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pageText(model.Pages[1]), "2\n") || !strings.Contains(pageText(model.Pages[1]), "8|3|2|1|2") {
		t.Fatalf("默认引用或占位符错误: %q", pageText(model.Pages[1]))
	}
	if !strings.Contains(pageText(model.Pages[2]), "9|3|3|2|2") {
		t.Fatalf("节内续号错误: %q", pageText(model.Pages[2]))
	}
}

func TestRomanNumberingAndContinuationAcrossSections(t *testing.T) {
	opts := smallOptions()
	opts.PageNumbers = true
	d := flow.New(opts)
	d.AddSection(flow.Section{StartPageNumber: ptr(39), PageNumberStyle: "roman", FirstPageHeader: ptr("First")})
	d.AddParagraph("Page one")
	d.AddSection(flow.Section{PageNumberStyle: "ROMAN"})
	d.AddParagraph("Page two")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pageText(model.Pages[0]), "xxxix / 2") || !strings.Contains(pageText(model.Pages[1]), "XL / 2") {
		t.Fatal("罗马编号或节间续号错误")
	}
	if strings.Contains(pageText(model.Pages[1]), "First") {
		t.Fatal("前节首页特殊页眉泄漏到后节")
	}
}

func TestSectionValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		section flow.Section
		want    string
	}{
		{"zero number", flow.Section{StartPageNumber: ptr(0)}, "正数"},
		{"bad style", flow.Section{PageNumberStyle: "unknown"}, "不支持"},
		{"bad size", flow.Section{PageSize: &creator.PageSize{Width: 100}}, "版心"},
		{"bad margins", flow.Section{Margins: &flow.Margins{Left: math.Inf(1)}}, "有限"},
		{"bad header", flow.Section{Header: ptr("bad\x01")}, "控制字符"},
		{"roman overflow", flow.Section{StartPageNumber: ptr(4000), PageNumberStyle: "roman"}, "3999"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := flow.New()
			d.AddSection(test.section)
			d.AddParagraph("text")
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误=%v，期望包含 %q", err, test.want)
			}
		})
	}
	d := flow.New()
	d.Options.ReferencePageNumbers = "unknown"
	if _, err := d.Build(context.Background()); err == nil {
		t.Fatal("未拒绝未知引用编号模式")
	}
	d = flow.New()
	d.AddParagraph("Text")
	d.AddSection(flow.Section{})
	d.AddSection(flow.Section{})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 1 {
		t.Fatal("结尾分节生成空白页")
	}
}

func TestLandscapeTableContinuationKeepsSectionSettings(t *testing.T) {
	opts := smallOptions()
	opts.ReferencePageNumbers = "section"
	d := flow.New(opts)
	d.AddParagraph("Portrait")
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 200, Height: 100}, PageNumbers: ptr(true), Header: ptr("Landscape"), StartPageNumber: ptr(7)})
	rows := make([][]string, 24)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(i), "Table continuation"}
	}
	d.Blocks = append(d.Blocks, flow.Block{ID: "table", Kind: "table", Table: &flow.Table{Header: []string{"No.", "Description"}, Rows: rows}})
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 100, Height: 90}, Header: ptr("Portrait again")})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{PageRef: "table"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 5 {
		t.Fatalf("没有覆盖多页横向表格: %d", len(model.Pages))
	}
	for i := 1; i < len(model.Pages)-1; i++ {
		page := model.Pages[i]
		if page.Area.PhysicalBox.Width != 200 || page.Area.PhysicalBox.Height != 100 {
			t.Fatal("续表没有继承横向尺寸")
		}
		text := pageText(page)
		if !strings.Contains(text, "Landscape") || strings.Count(text, "Description") != 1 || !strings.Contains(text, fmt.Sprintf("%d / %d", i+6, len(model.Pages))) {
			t.Fatalf("续表设置丢失: %q", text)
		}
	}
	last := model.Pages[len(model.Pages)-1]
	if last.Area.PhysicalBox.Width != 100 || !strings.Contains(pageText(last), "7\n") {
		t.Fatalf("后续纵向节或表格引用错误: %q", pageText(last))
	}
}
