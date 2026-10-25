package flow_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func TestExplicitZeroSpacingAndIndentOverrideThemeAndChangePagination(t *testing.T) {
	models := make([]*creator.Document, 2)
	for i, explicit := range []bool{false, true} {
		opts := smallOptions()
		opts.Theme.Body.SpaceBefore, opts.Theme.Body.SpaceAfter, opts.Theme.Body.FirstLineIndent = 4, 8, 6
		d := flow.New(opts)
		style := flow.TextStyle{}
		if explicit {
			style.Set = flow.StyleSpaceBefore | flow.StyleSpaceAfter | flow.StyleFirstLineIndent
		}
		for j := 0; j < 8; j++ {
			d.AddParagraph(fmt.Sprintf("Row-%d", j), style)
		}
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		models[i] = model
		var texts []creator.Text
		for _, item := range model.Pages[0].Items {
			if text, ok := item.(creator.Text); ok {
				texts = append(texts, text)
			}
		}
		wantX := 16.0
		if explicit {
			wantX = 10
		}
		if len(texts) < 2 || texts[0].X != wantX {
			t.Fatalf("显式零缩进未生效: explicit=%v texts=%+v", explicit, texts)
		}
		gap := texts[1].Y - texts[0].Y
		wantGap := 12 * 25.4 / 72 * 1.6
		if !explicit {
			wantGap += 12
		}
		if math.Abs(gap-wantGap) > 0.001 {
			t.Fatalf("段间距=%g，期望 %g", gap, wantGap)
		}
	}
	if len(models[1].Pages) != 1 || len(models[0].Pages) <= 1 {
		t.Fatalf("清除间距未影响分页: inherited=%d explicit=%d", len(models[0].Pages), len(models[1].Pages))
	}
}

func TestNamedStylesApplyParentsBeforeLocalOverridesAndPreserveInput(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.FirstLineIndent = 5
	blue, red := &creator.Color{B: 200}, &creator.Color{R: 200}
	opts.Styles = []flow.NamedStyle{
		{Name: "child", Style: flow.TextStyle{BasedOn: "base", Size: 15, Set: flow.StyleSpaceAfter | flow.StyleFirstLineIndent}},
		{Name: "base", Style: flow.TextStyle{Size: 14, LineHeight: 1.4, SpaceBefore: 3, SpaceAfter: 6, FirstLineIndent: 7, Align: "right", Color: blue}},
		{Name: "leaf", Style: flow.TextStyle{BasedOn: "child"}},
	}
	d := flow.New(opts)
	d.AddParagraph("First", flow.TextStyle{BasedOn: "leaf", Align: "left", Color: red})
	d.AddParagraph("Second", flow.TextStyle{BasedOn: "leaf", Align: "left", SpaceBefore: 0, Set: flow.StyleSpaceBefore})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, second := model.Pages[0].Items[0].(creator.Text), model.Pages[0].Items[1].(creator.Text)
	if first.X != 10 || second.X != 10 || first.FillColor != red || second.FillColor != blue || math.Abs(first.Size-15*25.4/72) > 0.001 {
		t.Fatalf("继承优先级错误: %+v / %+v", first, second)
	}
	if math.Abs(second.Y-first.Y-15*25.4/72*1.4) > 0.001 {
		t.Fatal("多层继承丢失零间距")
	}
	if d.Blocks[0].Style.BasedOn != "leaf" || d.Blocks[0].Style.Size != 0 || d.Options.Styles[0].Style.BasedOn != "base" {
		t.Fatal("解析样式修改了输入")
	}
	secondBuild, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(model, secondBuild) {
		t.Fatal("重复排版的样式结果不一致")
	}
}

func TestNamedCellStyleOverridesTableSpacingAndColumnAlignment(t *testing.T) {
	opts := smallOptions()
	opts.Styles = []flow.NamedStyle{
		{Name: "table", Style: flow.TextStyle{Size: 11, SpaceBefore: 2, SpaceAfter: 2, FirstLineIndent: 5}},
		{Name: "cell-base", Style: flow.TextStyle{SpaceBefore: 4, SpaceAfter: 4, FirstLineIndent: 6, Align: "right"}},
		{Name: "cell-flat", Style: flow.TextStyle{BasedOn: "cell-base", Align: "left", Set: flow.StyleSpaceBefore | flow.StyleSpaceAfter | flow.StyleFirstLineIndent}},
	}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Kind: "table", Style: &flow.TextStyle{BasedOn: "table"}, Table: &flow.Table{
		Columns: []flow.TableColumn{{Align: "right"}, {Align: "right"}},
		Grid:    [][]flow.TableCell{{{Text: "Left", Style: &flow.TextStyle{BasedOn: "cell-flat"}}, {Text: "Right"}}},
	}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && textValue(text) == "Left" {
			found = true
			if math.Abs(text.X-11.5) > 0.001 || math.Abs(text.Size-11*25.4/72) > 0.001 {
				t.Fatalf("单元格样式未覆盖列对齐或缩进: %+v", text)
			}
		}
	}
	if !found {
		t.Fatal("单元格文字丢失")
	}
	if d.Blocks[0].Table.Grid[0][0].Style.BasedOn != "cell-flat" {
		t.Fatal("修改了单元格输入")
	}
}

func TestThemeNamedStylesAndExplicitZeroCaptionGap(t *testing.T) {
	opts := smallOptions()
	opts.Styles = []flow.NamedStyle{{Name: "body", Style: opts.Theme.Body}, {Name: "compact", Style: flow.TextStyle{Set: flow.StyleSpaceBefore | flow.StyleSpaceAfter, Align: "left"}}}
	opts.Theme.Body = flow.TextStyle{BasedOn: "body"}
	opts.Theme.Caption = flow.TextStyle{BasedOn: "compact", Size: 10, LineHeight: 1.3}
	d := flow.New(opts)
	zero := flow.TextStyle{Set: flow.StyleSpaceBefore | flow.StyleSpaceAfter}
	d.Blocks = []flow.Block{{Kind: "fixed", Style: &zero, Fixed: &flow.Fixed{Height: 10, Caption: "Caption", Items: []creator.Item{fixedRect(0, 0, 10, 10)}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	caption := model.Pages[0].Items[1].(creator.Text)
	if caption.X != 10 || caption.Y < 20 || caption.Y > 21 {
		t.Fatalf("主题继承或图注零间距未生效: %+v", caption)
	}
}

func TestNamedTOCStylesResolveBeforePaginationAndKeepReferences(t *testing.T) {
	opts := smallOptions()
	opts.Styles = []flow.NamedStyle{{Name: "compact", Style: flow.TextStyle{Size: 10, Set: flow.StyleSpaceBefore | flow.StyleSpaceAfter | flow.StyleFirstLineIndent}}}
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{Kind: "toc", TOC: &flow.TOC{}, Style: &flow.TextStyle{BasedOn: "compact"}, PageBreakAfter: true})
	for i := 0; i < 10; i++ {
		d.Blocks = append(d.Blocks, flow.Block{Kind: "heading", Level: 1, Text: fmt.Sprintf("Chapter %d", i), ID: fmt.Sprintf("c%d", i), PageBreakBefore: true})
		d.AddParagraph("Body")
	}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	links := 0
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && len(text.Actions) > 0 {
			links++
			if math.Abs(text.Size-10*25.4/72) > 0.001 || text.Actions[0].Goto == nil {
				t.Fatalf("目录样式或导航丢失: %+v", text)
			}
		}
	}
	if links < 20 {
		t.Fatalf("紧凑目录未容纳十个条目及页码: links=%d", links)
	}
}

func TestNamedStyleErrorsAreDeterministicAndLocateSource(t *testing.T) {
	for _, test := range []struct {
		name   string
		styles []flow.NamedStyle
		block  flow.Block
		want   string
	}{
		{"duplicate", []flow.NamedStyle{{Name: "x"}, {Name: "x"}}, flow.Block{Text: "X"}, "styles[1]"},
		{"empty", []flow.NamedStyle{{}}, flow.Block{Text: "X"}, "样式名不能为空"},
		{"whitespace", []flow.NamedStyle{{Name: "bad name"}}, flow.Block{Text: "X"}, "空白"},
		{"missing unused", []flow.NamedStyle{{Name: "x", Style: flow.TextStyle{BasedOn: "missing"}}}, flow.Block{Text: "X"}, "不存在"},
		{"cycle", []flow.NamedStyle{{Name: "a", Style: flow.TextStyle{BasedOn: "b"}}, {Name: "b", Style: flow.TextStyle{BasedOn: "a"}}}, flow.Block{Text: "X"}, "循环继承"},
		{"negative unused", []flow.NamedStyle{{Name: "x", Style: flow.TextStyle{SpaceAfter: -1}}}, flow.Block{Text: "X"}, "有限非负数"},
		{"zero size", nil, flow.Block{Text: "X", Style: &flow.TextStyle{Set: flow.StyleSize}}, "字号和行高"},
		{"zero height", nil, flow.Block{Text: "X", Style: &flow.TextStyle{Set: flow.StyleLineHeight}}, "字号和行高"},
		{"invalid mask", nil, flow.Block{Text: "X", Style: &flow.TextStyle{Set: 1 << 12}}, "未知字段"},
		{"nan", nil, flow.Block{Text: "X", Style: &flow.TextStyle{SpaceAfter: math.NaN()}}, "有限非负数"},
		{"missing block", nil, flow.Block{Text: "X", Style: &flow.TextStyle{BasedOn: "missing"}}, "blocks[0].style"},
		{"missing cell", nil, flow.Block{Kind: "table", Table: &flow.Table{Grid: [][]flow.TableCell{{{Text: "X", Style: &flow.TextStyle{BasedOn: "missing"}}}}}}, "blocks[0].table.grid[0][0].style"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := smallOptions()
			opts.Styles = test.styles
			d := flow.New(opts)
			d.Blocks = []flow.Block{test.block}
			var previous string
			for i := 0; i < 3; i++ {
				_, err := d.Build(context.Background())
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("错误=%v，期望 %q", err, test.want)
				}
				if i > 0 && previous != err.Error() {
					t.Fatal("错误不稳定")
				}
				previous = err.Error()
			}
		})
	}
}

func TestNamedStyleInheritanceLimitDoesNotDependOnDefinitionOrder(t *testing.T) {
	for _, count := range []int{64, 65} {
		for _, reverse := range []bool{false, true} {
			opts := smallOptions()
			for i := 0; i < count; i++ {
				style := flow.TextStyle{}
				if i+1 < count {
					style.BasedOn = fmt.Sprintf("s%d", i+1)
				}
				opts.Styles = append(opts.Styles, flow.NamedStyle{Name: fmt.Sprintf("s%d", i), Style: style})
			}
			if reverse {
				for i, j := 0, len(opts.Styles)-1; i < j; i, j = i+1, j-1 {
					opts.Styles[i], opts.Styles[j] = opts.Styles[j], opts.Styles[i]
				}
			}
			d := flow.New(opts)
			d.AddParagraph("Text", flow.TextStyle{BasedOn: "s0"})
			_, err := d.Build(context.Background())
			if count == 64 && err != nil {
				t.Fatal(err)
			}
			if count == 65 && (err == nil || !strings.Contains(err.Error(), "超过 64")) {
				t.Fatalf("未限制继承深度: %v", err)
			}
		}
	}
}
