package flow_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

// 空白行占满一栏后，正文须继续到下一栏或下一页，不能被当成整页也排不下。
func TestBlankLinesAdvanceParagraphToFreshColumnOrPage(t *testing.T) {
	for _, columns := range []int{1, 2} {
		t.Run(map[int]string{1: "page", 2: "column"}[columns], func(t *testing.T) {
			opts := smallOptions()
			opts.Columns = flow.Columns{Count: columns, Gap: 8}
			d := flow.New(opts)
			d.AddParagraph(strings.Repeat("\n", 10))
			d.Blocks = append(d.Blocks, flow.Block{ID: "body", Text: "Visible"})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			wantPage, wantLeft := 1, 10.0
			if columns == 2 {
				wantPage, wantLeft = 0, 54
			}
			anchor := model.Bookmarks[0].Goto
			if len(model.Pages) != wantPage+1 || anchor.Page != wantPage || anchor.Top == nil || math.Abs(*anchor.Top-10) > 0.001 {
				t.Fatalf("正文未进入新栏或新页顶部: pages=%d anchor=%+v", len(model.Pages), anchor)
			}
			if strings.TrimSpace(pageText(model.Pages[wantPage])) != "Visible" {
				t.Fatal("换栏或换页丢失或重复了正文")
			}
			for _, item := range model.Pages[wantPage].Items {
				if text, ok := item.(creator.Text); ok && textValue(text) == "Visible" && math.Abs(text.X-wantLeft) > 0.001 {
					t.Fatalf("正文栏位置错误: %+v", text)
				}
			}
		})
	}
}

func TestBlankOnlyParagraphPreservesHeightAcrossPages(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Size, opts.Theme.Body.LineHeight, opts.Theme.Body.SpaceAfter = 12, 1.5, 0
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("\n", 25))
	d.Blocks = append(d.Blocks, flow.Block{ID: "body", Text: "Visible"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 每页容纳 11 个 6.35mm 高的空白行，第三页仍须保留剩余三个空白行。
	anchor := model.Bookmarks[0].Goto
	if len(model.Pages) != 3 || anchor.Page != 2 || anchor.Top == nil || math.Abs(*anchor.Top-29.05) > 0.001 {
		t.Fatalf("跨页空白行的数量或高度未保留: pages=%d anchor=%+v", len(model.Pages), anchor)
	}
	if len(model.Pages[0].Items) != 0 || len(model.Pages[1].Items) != 0 || strings.TrimSpace(pageText(model.Pages[2])) != "Visible" {
		t.Fatal("跨页空白行产生了额外图元或丢失正文")
	}
}

// 显式分页须识别仅含空白行的页，包括空白行已在前一栏、当前栏刚开始的情况。
func TestExplicitPageBreaksAfterBlankLines(t *testing.T) {
	for _, mode := range []string{"block", "before", "after", "empty second column"} {
		t.Run(mode, func(t *testing.T) {
			opts := smallOptions()
			if mode == "empty second column" {
				opts.Columns = flow.Columns{Count: 2, Gap: 8}
			}
			d := flow.New(opts)
			d.AddParagraph(strings.Repeat("\n", 5))
			switch mode {
			case "block":
				d.AddPageBreak()
			case "after":
				d.Blocks[0].PageBreakAfter = true
			case "empty second column":
				d.AddColumnBreak()
				d.AddPageBreak()
			}
			d.Blocks = append(d.Blocks, flow.Block{ID: "body", Text: "Visible", PageBreakBefore: mode == "before"})
			d.AddPageBreak()
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			anchor := model.Bookmarks[0].Goto
			if len(model.Pages) != 2 || anchor.Page != 1 || anchor.Top == nil || math.Abs(*anchor.Top-10) > 0.001 || anchor.Left != nil && *anchor.Left != 10 {
				t.Fatalf("显式分页被忽略或生成了尾部空页: pages=%d anchor=%+v", len(model.Pages), anchor)
			}
		})
	}
}

// 空白行后的整体图元必须完整落入版心；图片与固定区域共用整体高度检查。
func TestBlankLinesMoveWholeFiguresWithinPageBounds(t *testing.T) {
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgDr8AAAGjATO977V+AAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"fixed", "image"} {
		t.Run(kind, func(t *testing.T) {
			d := flow.New(smallOptions())
			d.AddParagraph(strings.Repeat("\n", 10))
			block := flow.Block{ID: "figure", Kind: kind}
			if kind == "fixed" {
				block.Fixed = &flow.Fixed{Width: 50, Height: 50, Items: []creator.Item{fixedRect(0, 0, 50, 50)}}
			} else {
				block.Image = &flow.Image{Data: image, Width: 50, Height: 50}
			}
			d.Blocks = append(d.Blocks, block)
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Pages) != 2 || model.Bookmarks[0].Goto.Page != 1 {
				t.Fatalf("整体图元没有换页: pages=%d bookmarks=%+v", len(model.Pages), model.Bookmarks)
			}
			var top, bottom float64
			switch item := model.Pages[1].Items[0].(type) {
			case creator.PageBlock:
				path := item.Items[0].(creator.Path)
				top, bottom = path.Y, path.Y+path.Height
			case creator.Image:
				top, bottom = item.Y, item.Y+item.Height
			default:
				t.Fatalf("整体图元类型错误: %T", item)
			}
			if math.Abs(top-10) > 0.001 || bottom > 80.001 {
				t.Fatalf("图元越出版心: top=%g bottom=%g", top, bottom)
			}
			var output bytes.Buffer
			if err := d.Write(context.Background(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
				t.Fatal(err)
			}
			v, err := validator.New(validator.WithMode(validator.ModeStrict))
			if err != nil {
				t.Fatal(err)
			}
			report := v.ValidateReader(context.Background(), bytes.NewReader(output.Bytes()), "blank-figure.ofd")
			if report.HasErrors() || report.HasWarnings() {
				t.Fatalf("严格校验失败: %+v", report.Issues)
			}
		})
	}
}
