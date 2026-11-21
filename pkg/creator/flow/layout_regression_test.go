package flow_test

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 空文档至少保留一个物理页，纯空白只推进游标而不输出非法空白文字对象。
func TestLayoutEmptyDocumentsAndWhitespaceNeverEmitWhitespaceText(t *testing.T) {
	for _, source := range []string{"", " \t \n\n \n"} {
		d := flow.New()
		if source != "" {
			d.AddParagraph(source)
		}
		model, err := d.Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(model.Pages) != 1 {
			t.Fatalf("空文档或短空白文档应只有一页: %d", len(model.Pages))
		}
		for _, text := range modelTexts(model) {
			if strings.TrimSpace(textValue(text)) == "" {
				t.Fatalf("生成了纯空白文字对象: %+v", text)
			}
		}
	}
}

// 旧排版入口的英文词完整性约束必须由实际使用的流式入口继续保护。
func TestWrappingKeepsLatinWordsIntactAcrossStyledRuns(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Width = 60
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{
		{Text: "alpha be"},
		{Text: "ta", Bold: true},
		{Text: " gamma delta epsilon zeta eta theta"},
	}}}
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	var current strings.Builder
	var baseline float64
	for _, text := range modelTexts(model) {
		y := text.Y + *text.TextCodes[0].Y
		if current.Len() > 0 && y != baseline {
			lines = append(lines, current.String())
			current.Reset()
		}
		current.WriteString(textValue(text))
		baseline = y
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	if len(lines) < 2 {
		t.Fatal("测试没有触发自动换行")
	}
	got := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if want := "alpha beta gamma delta epsilon zeta eta theta"; got != want {
		t.Fatalf("自动换行拆开了英文词: %q，期望 %q", got, want)
	}
}

// 相邻不同目标、空目标和表格单元格的链接均须保持独立的 CLICK 动作。
func TestLayoutKeepsAdjacentAndEmptyLinksInParagraphsAndCells(t *testing.T) {
	runs := []flow.Run{
		{Text: "first", Href: "https://a.example"},
		{Text: "second", Href: "https://b.example"},
		{Text: "empty", Link: true},
	}
	for _, table := range []bool{false, true} {
		name := "paragraph"
		if table {
			name = "table"
		}
		t.Run(name, func(t *testing.T) {
			d := flow.New()
			if table {
				d.AddTable(flow.Table{Grid: [][]flow.TableCell{{{Runs: runs}}}})
			} else {
				d.Blocks = []flow.Block{{Runs: runs}}
			}
			model, err := d.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			seen := make(map[string]bool)
			for _, text := range modelTexts(model) {
				value := textValue(text)
				seen[value] = true
				switch value {
				case "first", "second":
					target := "https://a.example"
					if value == "second" {
						target = "https://b.example"
					}
					if len(text.Actions) != 1 || text.Actions[0].Event != creator.ActionEventClick ||
						text.Actions[0].URI == nil || text.Actions[0].URI.URI != target {
						t.Fatalf("链接目标丢失或被合并: %q / %+v", value, text.Actions)
					}
				case "empty":
					if len(text.Actions) != 0 {
						t.Fatalf("空目标生成了动作: %+v", text.Actions)
					}
					color := text.FillColor
					if color == nil || color.R != 0x0b || color.G != 0x57 || color.B != 0xd0 {
						t.Fatalf("空目标链接丢失颜色: %+v", color)
					}
				default:
					t.Fatalf("相邻链接被合并: %q", value)
				}
			}
			if !seen["first"] || !seen["second"] || !seen["empty"] {
				t.Fatalf("链接文字缺失: %v", seen)
			}
		})
	}
}

// 行内代码背景必须先于文字绘制，并覆盖带有显式基线的完整文字边界。
func TestInlineCodeBackgroundPrecedesAndCoversText(t *testing.T) {
	d := flow.New()
	d.Blocks = []flow.Block{{Runs: []flow.Run{
		{Text: "prefix "}, {Text: "a.zip", Code: true}, {Text: " suffix"},
	}}}
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var backgrounds []creator.Path
	found := false
	for _, item := range model.Pages[0].Items {
		switch item := item.(type) {
		case creator.Path:
			if color := item.FillColor; color != nil && color.R == 0xe9 && color.G == 0xec && color.B == 0xf0 {
				backgrounds = append(backgrounds, item)
			}
		case creator.Text:
			if strings.TrimSpace(textValue(item)) != "a.zip" {
				continue
			}
			found = true
			if len(backgrounds) != 1 {
				t.Fatalf("行内代码背景没有先于文字绘制: %d", len(backgrounds))
			}
			background := backgrounds[0]
			if item.X < background.X-0.001 || item.X+item.Width > background.X+background.Width+0.001 ||
				item.Y < background.Y-0.001 || item.Y+item.Height > background.Y+background.Height+0.001 {
				t.Fatalf("代码文字越出背景: %+v / %+v", item, background)
			}
			baseline := item.Y + *item.TextCodes[0].Y
			if baseline < background.Y || baseline > background.Y+background.Height {
				t.Fatalf("代码基线越出背景: %g / %+v", baseline, background)
			}
		}
	}
	if !found {
		t.Fatal("未生成行内代码")
	}
}
