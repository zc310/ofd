package flow_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

// Markdown 在同一条 flow 路径中测量与绘制，不能按源码宽度排版后再替换文字。
func TestTableMarkdownCellPreservesInlineStylesLinksAndNewlines(t *testing.T) {
	d := flow.New()
	source := "**Bold** *Italic* ~~Strike~~ [`Code`](https://example.com)\nNext"
	d.AddTable(flow.Table{Grid: [][]flow.TableCell{{{Format: "markdown", Text: source}}}})
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	bold, italic, code, linked, next := false, false, false, false, false
	firstY := 0.0
	for _, item := range model.Pages[0].Items {
		text, ok := item.(creator.Text)
		if !ok {
			continue
		}
		value := strings.TrimSpace(textValue(text))
		switch value {
		case "Bold":
			bold, firstY = text.Weight == 700 || text.Font == "Flow-Bold", text.Y
		case "Italic":
			italic = text.Italic || text.Font == "Flow-Italic"
		case "Code":
			for _, font := range model.Fonts {
				code = code || font.Name == text.Font && font.FixedWidth
			}
			linked = len(text.Actions) == 1 && text.Actions[0].URI != nil && text.Actions[0].URI.URI == "https://example.com"
		case "Next":
			next = text.Y > firstY
		}
	}
	if !bold || !italic || !code || !linked || !next || strings.Contains(pageText(model.Pages[0]), "**") {
		t.Fatalf("单元格样式丢失: bold=%v italic=%v code=%v link=%v newline=%v", bold, italic, code, linked, next)
	}
	if d.Blocks[0].Table.Grid[0][0].Text != source || len(d.Blocks[0].Table.Grid[0][0].Runs) != 0 {
		t.Fatal("Markdown 解析修改了调用方输入")
	}
}

func TestTableMarkdownRepeatsHeadersAndWritesDeterministically(t *testing.T) {
	d := flow.New(smallOptions())
	grid := make([][]flow.TableCell, 1, 31)
	grid[0] = []flow.TableCell{{Text: "**Header**", Format: "markdown"}}
	for i := range 30 {
		grid = append(grid, []flow.TableCell{{Text: fmt.Sprintf("**Row-%02d**", i), Format: "markdown"}})
	}
	d.AddTable(flow.Table{HeaderRows: 1, Grid: grid, Caption: "Results", RepeatCaption: true})
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 2 {
		t.Fatal("长表未分页")
	}
	var all strings.Builder
	for _, page := range model.Pages {
		text := pageText(page)
		if !strings.Contains(text, "Header") || strings.Contains(text, "**") {
			t.Fatalf("续页表头丢失或未解析 Markdown: %q", text)
		}
		all.WriteString(text)
	}
	for i := range 30 {
		if strings.Count(all.String(), fmt.Sprintf("Row-%02d", i)) != 1 {
			t.Fatalf("第 %d 行丢失或重复", i)
		}
	}
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(t.Context(), output, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("Markdown 表格输出不确定")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(first.Bytes()), "markdown-table.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("Markdown 表格严格校验失败: %+v", report.Issues)
	}
}

func TestTableCellDefaultsToPlainTextAndRejectsConflictingFormats(t *testing.T) {
	d := flow.New()
	d.AddTable(flow.Table{Rows: [][]string{{"**literal**"}}})
	model, err := d.Build(t.Context())
	if err != nil || !strings.Contains(pageText(model.Pages[0]), "**literal**") {
		t.Fatalf("默认纯文本被误解析: %v", err)
	}
	for _, cell := range []flow.TableCell{
		{Text: "X", Format: "html"},
		{Text: "**X**", Format: "markdown", Runs: []flow.Run{{Text: "X"}}},
		{Format: "text", Runs: []flow.Run{{Text: "X"}}},
	} {
		d := flow.New()
		d.AddTable(flow.Table{Grid: [][]flow.TableCell{{cell}}})
		if _, err := d.Build(t.Context()); err == nil || !strings.Contains(err.Error(), "grid[0][0]") {
			t.Fatalf("未拒绝错误格式或错误缺少位置: %v", err)
		}
	}
}

// 分隔线使用一行高度，不应把块分隔产生的换行额外算成空行。
func TestTableMarkdownRuleUsesOneLineAndStaysInsideCell(t *testing.T) {
	gaps := make([]float64, 0, 2)
	for _, source := range []string{"Before\nAfter", "Before\n\n***\n\nAfter"} {
		d := flow.New()
		d.AddTable(flow.Table{Grid: [][]flow.TableCell{{{Format: "markdown", Text: source}}}})
		model, err := d.Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		beforeY, afterY, ruleY := 0.0, 0.0, 0.0
		for _, item := range model.Pages[0].Items {
			switch value := item.(type) {
			case creator.Text:
				switch strings.TrimSpace(textValue(value)) {
				case "Before":
					beforeY = value.Y
				case "After":
					afterY = value.Y
				}
			case creator.Path:
				if value.X > 20 && value.Width > 160 && value.Height < 1 {
					ruleY = value.Y
				}
			}
		}
		gaps = append(gaps, afterY-beforeY)
		if strings.Contains(source, "***") && (ruleY <= beforeY || ruleY >= afterY) {
			t.Fatalf("分隔线未落在两行之间: before=%v rule=%v after=%v", beforeY, ruleY, afterY)
		}
	}
	if diff := gaps[1] - 2*gaps[0]; diff < -0.001 || diff > 0.001 {
		t.Fatalf("分隔线多占空行: %v", gaps)
	}
}
