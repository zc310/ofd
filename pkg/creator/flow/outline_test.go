package flow_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func TestGeneratedOutlinesUseFinalHeadingPagesAndHierarchy(t *testing.T) {
	opts := smallOptions()
	opts.GenerateOutlines = true
	d := flow.New(opts)
	d.Metadata.Outlines = make([]creator.Outline, 1, 8)
	d.Metadata.Outlines[0] = creator.Outline{Title: "Manual", Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 0, Type: "Fit"}}}}
	d.AddHeading(1, "Report")
	d.AddParagraph(strings.Repeat("line\n", 7))
	d.AddHeading(3, "Moved heading")
	d.AddParagraph("First line\nSecond line\nThird line")
	d.AddPageBreak()
	d.Blocks = append(d.Blocks, flow.Block{Kind: "heading", Level: 2, Runs: []flow.Run{{Text: "Mixed "}, {Text: "heading", Bold: true}}})
	d.AddHeading(1, "Appendix")
	d.AddHeading(2, "\n")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Outlines) != 3 || model.Outlines[0].Title != "Manual" || model.Outlines[1].Title != "Report" || model.Outlines[2].Title != "Appendix" {
		t.Fatalf("大纲顶层错误: %+v", model.Outlines)
	}
	children := model.Outlines[1].Children
	if len(children) != 2 || children[0].Title != "Moved heading" || children[1].Title != "Mixed heading" {
		t.Fatalf("跳级标题层级错误: %+v", children)
	}
	var check func([]creator.Outline)
	check = func(nodes []creator.Outline) {
		for _, node := range nodes {
			if node.Title == "Manual" {
				continue
			}
			if len(node.Actions) != 1 || node.Actions[0].Goto == nil {
				t.Fatalf("大纲缺少跳转: %+v", node)
			}
			gotoAction := node.Actions[0].Goto
			if gotoAction.Page < 0 || gotoAction.Page >= len(model.Pages) || gotoAction.Type != "FitH" || gotoAction.Top == nil {
				t.Fatalf("大纲目的地错误: %+v", gotoAction)
			}
			found := false
			for _, item := range model.Pages[gotoAction.Page].Items {
				if text, ok := item.(creator.Text); ok && strings.Contains(node.Title, strings.TrimSpace(textValue(text))) {
					if text.Y+0.001 >= *gotoAction.Top && text.Y-*gotoAction.Top < 10 {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("大纲未指向标题实际位置: title=%q dest=%+v", node.Title, gotoAction)
			}
			check(node.Children)
		}
	}
	check(model.Outlines)
	if children[0].Actions[0].Goto.Page == 0 {
		t.Fatal("标题跳转仍使用分页前的页码")
	}
	if len(d.Metadata.Outlines) != 1 {
		t.Fatal("自动大纲修改了输入元数据")
	}
	var output bytes.Buffer
	if err := d.Write(context.Background(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(output.Bytes()), "outline.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("自动大纲严格校验失败: %+v", report.Issues)
	}
}

func TestOutlineGenerationIsOptional(t *testing.T) {
	d := flow.New()
	d.AddHeading(3, "A skipped-level title")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Outlines) != 0 {
		t.Fatal("未启用时生成了自动大纲")
	}
	d.Options.GenerateOutlines = true
	model, err = d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Outlines) != 1 || model.Outlines[0].Title != "A skipped-level title" {
		t.Fatalf("首个跳级标题未成为根节点: %+v", model.Outlines)
	}
}
