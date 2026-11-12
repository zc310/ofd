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
)

// 每页代码只使用一个面板，标签只出现一次，文字和基线不得越出背景。
func TestCodePanelsPaginateInsideMarginsAndLabelOnlyFirstFragment(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Height = 60
	d := flow.New(opts)
	var code strings.Builder
	for i := range 25 {
		fmt.Fprintf(&code, "    line%02d\n", i)
	}
	d.Blocks = []flow.Block{{Kind: "code", Text: code.String(), CodeLang: "go", Indent: 6, Orphans: 1, Widows: 1}}
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 3 {
		t.Fatalf("代码未跨页: %d", len(model.Pages))
	}
	labels, count := 0, 0
	for _, page := range model.Pages {
		var panel creator.Path
		panels := 0
		for _, item := range page.Items {
			if path, ok := item.(creator.Path); ok {
				panel = path
				panels++
			}
		}
		if panels != 1 {
			t.Fatalf("每页应恰好一个面板，实际 %d", panels)
		}
		if panel.X < opts.MarginLeft+6 || panel.Y < opts.MarginTop-0.001 || panel.Y+panel.Height > opts.PageSize.Height-opts.MarginBottom+0.001 {
			t.Fatalf("面板越出版心: %+v", panel)
		}
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			if textValue(text) == "go" {
				labels++
			} else {
				count++
				if text.X <= panel.X+2 {
					t.Fatal("代码行首空白丢失")
				}
			}
			if text.Y < panel.Y || text.Y+text.Height > panel.Y+panel.Height+0.001 || text.X+text.Width > panel.X+panel.Width+0.001 {
				t.Fatalf("代码文字越出背景: %+v / %+v", text, panel)
			}
		}
	}
	if labels != 1 || count != 25 {
		t.Fatalf("标签或代码行丢失/重复: labels=%d lines=%d", labels, count)
	}
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(t.Context(), output, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("代码面板输出不确定")
	}
}

func TestInlineCodeUsesMonoBackgroundAndKeepsLinkAndStrike(t *testing.T) {
	d := flow.New()
	d.Blocks = []flow.Block{{Runs: []flow.Run{
		{Text: "prefix "}, {Text: "a.zip", Code: true, Strike: true, Href: "https://example.com"}, {Text: " tail"},
	}}}
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fonts := map[string]creator.Font{}
	for _, font := range model.Fonts {
		fonts[font.Name] = font
	}
	found, backgrounds := false, 0
	for _, item := range model.Pages[0].Items {
		if path, ok := item.(creator.Path); ok && path.FillColor != nil && path.FillColor.R == 0xe9 {
			backgrounds++
		}
		if text, ok := item.(creator.Text); ok && strings.Contains(textValue(text), "a.zip") {
			found = fonts[text.Font].FixedWidth && len(text.Actions) == 1
		}
	}
	if !found || backgrounds != 1 {
		t.Fatalf("行内代码缺少等宽字体、动作或背景: found=%v backgrounds=%d", found, backgrounds)
	}
}

func TestLogicalFontsAreExplicitAndEmbeddedFontsRemainStrict(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.Fonts = []creator.Font{{Name: "Logical", FontName: "sans-serif", FamilyName: "sans-serif"}}
	opts.Theme.Body.Font = "Logical"
	opts.BoldFont, opts.ItalicFont, opts.BoldItalicFont = "", "", ""
	d := flow.New(opts)
	d.AddParagraph("中文正文")
	if _, err := d.Build(t.Context()); err == nil {
		t.Fatal("默认模式不得接受没有数据的字体")
	}
	d.Options.AllowLogicalFonts = true
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Fonts) != 1 || len(model.Fonts[0].Data) != 0 {
		t.Fatal("逻辑字体被替换为本机嵌入字体")
	}
	d = flow.New()
	d.Options.AllowLogicalFonts = true
	d.AddParagraph("中文正文")
	if _, err := d.Build(t.Context()); err == nil {
		t.Fatal("允许逻辑字体不能绕过嵌入字体缺字检查")
	}
}

func TestQuoteAndNestedListIndentSurvivesPagination(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Height = 50
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Kind: "quote", Indent: 6, Text: strings.Repeat("quoted line\n", 20)}}
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && math.Abs(text.X-(opts.MarginLeft+12)) > 0.001 {
				t.Fatalf("引用续页缩进丢失: %+v", text)
			}
		}
	}
	d.Blocks = []flow.Block{{Kind: "list_item", Indent: 12, Marker: "3.", Text: "first\nsecond"}}
	model, err = d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	x := -1.0
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && textValue(text) != "3." {
			if x >= 0 && math.Abs(x-text.X) > 0.001 {
				t.Fatal("列表续行没有对齐正文")
			}
			x = text.X
		}
	}
}

func TestMarkdownBlockValidationRejectsOverflowAndInvalidKinds(t *testing.T) {
	for _, block := range []flow.Block{
		{Kind: "code", Runs: []flow.Run{{Text: "x"}}},
		{Kind: "thematic_break", Text: "hidden"},
		{Text: "x", Indent: math.NaN()},
		{Text: "x", Indent: 1000},
		{Text: "x", CodeLang: "go"},
	} {
		d := flow.New()
		d.Blocks = []flow.Block{block}
		if _, err := d.Build(t.Context()); err == nil {
			t.Fatalf("无效块未报错: %+v", block)
		}
	}
}
