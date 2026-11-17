package flow_test

import (
	"bytes"
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/spec"
	"github.com/zc310/ofd/pkg/validator"
)

// 四种变体共用容器布局，只有标题与色条变色，文字仍可提取且输出确定。
func TestCalloutVariantsProduceDeterministicStrictOFD(t *testing.T) {
	for _, variant := range []string{"", "info", "warning", "danger", "success"} {
		t.Run(variant, func(t *testing.T) {
			d := flow.New()
			d.Options.GenerateOutlines = true
			d.AddCallout(flow.Callout{Variant: variant, Title: "Notice", Fill: &creator.Color{R: 250, G: 250, B: 250}, Blocks: []flow.Block{
				{Runs: []flow.Run{{Text: "Read "}, {Text: "carefully", Bold: true}, {Text: " online", Href: "https://example.com"}}},
				{Kind: "list_item", Text: "Save a backup"},
			}})
			original := d.Blocks[0].Callout.Blocks[0].Runs[0]
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Pages) != 1 || len(model.Outlines) != 0 || !strings.Contains(pageText(model.Pages[0]), "Notice") || !strings.Contains(pageText(model.Pages[0]), "Save a backup") {
				t.Fatalf("提示内容或大纲异常: %+v", model)
			}
			if !reflect.DeepEqual(original, d.Blocks[0].Callout.Blocks[0].Runs[0]) {
				t.Fatal("修改了输入")
			}
			for _, item := range model.Pages[0].Items {
				if path, ok := item.(creator.Path); ok {
					if path.CTM != nil {
						t.Fatal("未启用 ShowIcon 时不应输出图标")
					}
					if _, err := spec.ParsePathData(path.Data); err != nil {
						t.Fatal(err)
					}
				}
			}
			var a, b bytes.Buffer
			for _, out := range []*bytes.Buffer{&a, &b} {
				if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(a.Bytes(), b.Bytes()) {
				t.Fatal("输出不确定")
			}
			v, err := validator.New(validator.WithMode(validator.ModeStrict))
			if err != nil {
				t.Fatal(err)
			}
			report := v.ValidateReader(context.Background(), bytes.NewReader(a.Bytes()), "callout.ofd")
			if report.HasErrors() || report.HasWarnings() {
				t.Fatalf("严格校验失败: %+v", report.Issues)
			}
		})
	}
}

func TestCalloutIconsHaveDistinctShapesAndUseAccentColor(t *testing.T) {
	paths := map[string]bool{}
	accent := &creator.Color{R: 80, G: 80, B: 80}
	for _, variant := range []string{"info", "warning", "danger", "success"} {
		d := flow.New()
		d.AddCallout(flow.Callout{Variant: variant, Text: "Body", ShowIcon: true, AccentColor: accent})
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range model.Pages[0].Items {
			if path, ok := item.(creator.Path); ok && path.CTM != nil {
				if paths[path.Data] || !reflect.DeepEqual(path.FillColor, accent) {
					t.Fatalf("图标形状重复或未继承强调色: %s", variant)
				}
				paths[path.Data], found = true, true
			}
		}
		if !found {
			t.Fatalf("缺少图标: %s", variant)
		}
	}
}

// 图标只出现一次，不增加文字；纯文本和富文本简写复用相同正文排版。
func TestCalloutShorthandAndIconsAcrossColumns(t *testing.T) {
	for _, variant := range []string{"", "info", "warning", "danger", "success"} {
		for _, title := range []string{"", "A long notice title\nwith a second line"} {
			t.Run(variant+title, func(t *testing.T) {
				opts := smallOptions()
				opts.Columns = flow.Columns{Count: 2, Gap: 6}
				opts.Footnotes = []flow.Footnote{{ID: "source", Text: "Source note"}}
				d := flow.New(opts)
				callout := flow.Callout{Variant: variant, ShowIcon: true, Title: title, Runs: []flow.Run{{Text: "Opening"}, {Footnote: "source"}, {Text: "\n" + strings.Repeat("Body\n", 25)}}}
				if title != "" {
					callout.Blocks = []flow.Block{{Runs: callout.Runs}, {Text: "Second paragraph\n" + strings.Repeat("More body\n", 12)}}
					callout.Runs = nil
				}
				d.AddCallout(callout)
				model, err := d.Build(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				icons, notes := 0, 0
				for _, page := range model.Pages {
					notes += strings.Count(pageText(page), "Source note")
					for _, item := range page.Items {
						if path, ok := item.(creator.Path); ok && path.CTM != nil {
							icons++
							if _, err := spec.ParsePathData(path.Data); err != nil {
								t.Fatal(err)
							}
							if path.X < 13 || path.X+path.Width > 90 || path.Y < 10 || path.Y+path.Height > 80 {
								t.Fatalf("图标越界: %+v", path)
							}
						}
					}
				}
				if icons != 1 || notes != 1 {
					t.Fatalf("图标或脚注重复/缺失: icons=%d notes=%d", icons, notes)
				}
			})
		}
	}
	for _, body := range []flow.Callout{
		{Variant: "success", ShowIcon: true, Text: "Completed"},
		{Variant: "success", ShowIcon: true, Blocks: []flow.Block{{Kind: "list_item", Text: "Completed"}}},
	} {
		d := flow.New()
		d.AddCallout(body)
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pageText(model.Pages[0]), "Completed") {
			t.Fatal("简写正文缺失")
		}
	}
}

func TestCalloutRejectsConflictingShorthand(t *testing.T) {
	for _, c := range []flow.Callout{
		{Text: "Text", Runs: []flow.Run{{Text: "Run"}}},
		{Text: "Text", Blocks: []flow.Block{{Text: "Block"}}},
		{Runs: []flow.Run{{Text: "Run"}}, Blocks: []flow.Block{{Text: "Block"}}},
		{Text: "bad\x00"}, {Runs: []flow.Run{{Text: "bad\x00"}}},
	} {
		d := flow.New()
		d.AddCallout(c)
		if _, err := d.Build(context.Background()); err == nil {
			t.Fatal("未拒绝冲突或非法正文")
		}
	}
}

// 同一逻辑框在每个栏片段只有一条直色条，正文、标题和外框均不越界。
func TestCalloutContinuesAcrossPagesAndBalancedColumns(t *testing.T) {
	for _, columns := range []int{1, 2} {
		t.Run(strconv.Itoa(columns), func(t *testing.T) {
			opts := smallOptions()
			opts.Columns = flow.Columns{Count: columns, Gap: 6, Balance: true}
			d := flow.New(opts)
			accent := &creator.Color{R: 12, G: 34, B: 56}
			d.AddCallout(flow.Callout{Title: "Notice", AccentColor: accent, Blocks: []flow.Block{{Text: strings.Repeat("Body line\n", 38) + "Last line"}}})
			d.AddParagraph("After")
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Pages) < 2 {
				t.Fatal("未跨页")
			}
			titles, panels := 0, 0
			for _, page := range model.Pages {
				titles += strings.Count(pageText(page), "Notice")
				for _, item := range page.Items {
					switch v := item.(type) {
					case creator.Path:
						if reflect.DeepEqual(v.FillColor, accent) {
							panels++
							if v.Width != 1 || v.Y < 10-0.001 || v.Y+v.Height > 80.001 {
								t.Fatalf("色条越界: %+v", v)
							}
						}
					case creator.Text:
						if v.X < 10 || v.X+v.Width > 90.001 || v.Y < 10 || v.Y+v.Height > 80.001 {
							t.Fatalf("文字越界: %+v", v)
						}
					}
				}
			}
			if titles != 1 || panels < 3 {
				t.Fatalf("标题重复或片段缺失: titles=%d panels=%d", titles, panels)
			}
		})
	}
}

func TestCalloutShortContainerMovesTogetherAndPreservesReferencesAndFootnotes(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	opts.Styles = []flow.NamedStyle{{Name: "small", Style: flow.TextStyle{Size: 10}}}
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: "Source note"}}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	d.Blocks = append(d.Blocks, flow.Block{ID: "notice", Kind: "callout", Callout: &flow.Callout{Title: "Notice", Variant: "info", Blocks: []flow.Block{
		{Style: &flow.TextStyle{BasedOn: "small"}, Runs: []flow.Run{{Text: "Body"}, {Footnote: "source"}}},
		{Text: "Second paragraph"},
	}}})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "See "}, {PageRef: "notice"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bookmarkPages(model)["notice"] != 1 || strings.Contains(pageText(model.Pages[0]), "Notice") {
		t.Fatalf("短框被拆分: %v", bookmarkPages(model))
	}
	if !strings.Contains(pageText(model.Pages[1]), "Body") || !strings.Contains(pageText(model.Pages[1]), "Source note") {
		t.Fatal("正文或脚注未跟随")
	}
}

func TestCalloutTitleFollowsLongBodyOpeningOntoNextPage(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.SpaceAfter = 0
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 8))
	d.AddCallout(flow.Callout{Title: "Notice", Blocks: []flow.Block{{Text: strings.Repeat("Body\n", 25)}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pageText(model.Pages[0]), "Notice") || !strings.Contains(pageText(model.Pages[1]), "Notice") || !strings.Contains(pageText(model.Pages[1]), "Body") {
		t.Fatal("长框标题孤立在页尾")
	}
}

func TestCalloutExpansionKeepsFollowingBlockErrorIndex(t *testing.T) {
	d := flow.New()
	d.AddCallout(flow.Callout{Title: "Notice", Blocks: []flow.Block{{Text: "First"}, {Text: "Second"}}})
	d.Blocks = append(d.Blocks, flow.Block{Kind: "unknown"})
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "blocks[1]") {
		t.Fatalf("展开后错误索引不再指向源块: %v", err)
	}
}

func TestCalloutRejectsInvalidContent(t *testing.T) {
	for name, block := range map[string]flow.Block{
		"missing":         {Kind: "callout"},
		"wrong kind":      {Callout: &flow.Callout{Blocks: []flow.Block{{Text: "Body"}}}},
		"unknown variant": {Kind: "callout", Callout: &flow.Callout{Variant: "unknown", Blocks: []flow.Block{{Text: "Body"}}}},
		"empty":           {Kind: "callout", Callout: &flow.Callout{Title: "Title"}},
		"empty run":       {Kind: "callout", Callout: &flow.Callout{Title: "Title", Blocks: []flow.Block{{Runs: []flow.Run{{}}}}}},
		"negative indent": {Kind: "callout", Indent: -1, Callout: &flow.Callout{Blocks: []flow.Block{{Text: "Body"}}}},
		"conflict":        {Kind: "callout", Text: "Other", Callout: &flow.Callout{Blocks: []flow.Block{{Text: "Body"}}}},
		"nested":          {Kind: "callout", Callout: &flow.Callout{Blocks: []flow.Block{{Kind: "callout", Callout: &flow.Callout{}}}}},
		"break":           {Kind: "callout", Callout: &flow.Callout{Blocks: []flow.Block{{Text: "Body", PageBreakBefore: true}}}},
		"invalid text":    {Kind: "callout", Callout: &flow.Callout{Blocks: []flow.Block{{Text: "bad\x00"}}}},
		"missing style":   {Kind: "callout", Callout: &flow.Callout{Blocks: []flow.Block{{Text: "Body", Style: &flow.TextStyle{BasedOn: "missing"}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			d := flow.New()
			d.Blocks = []flow.Block{block}
			if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "blocks[0]") {
				t.Fatalf("未拒绝错误内容: %v", err)
			}
		})
	}
}
