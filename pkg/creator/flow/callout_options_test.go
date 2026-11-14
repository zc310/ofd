package flow_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func calloutPaths(model *creator.Document) []creator.Path {
	var paths []creator.Path
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if path, ok := item.(creator.Path); ok {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func TestCalloutAppearanceUsesThemeAndLocalOverrides(t *testing.T) {
	for _, appearance := range []string{"outline", "subtle", "minimal"} {
		t.Run(appearance, func(t *testing.T) {
			opts := flow.DefaultOptions()
			opts.Theme = flow.Bootstrap5Theme()
			d := flow.New(opts)
			d.AddCallout(flow.Callout{Variant: "success", Appearance: appearance, Text: "Completed"})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			paths := calloutPaths(model)
			if appearance == "minimal" {
				if len(paths) != 1 {
					t.Fatalf("minimal 不应绘制背景和细框: %d", len(paths))
				}
			} else {
				if len(paths) != 5 {
					t.Fatalf("缺少背景或细框: %d", len(paths))
				}
				fill := &creator.Color{R: 255, G: 255, B: 255}
				if appearance == "subtle" {
					fill = &creator.Color{R: 209, G: 231, B: 221}
				}
				if !reflect.DeepEqual(paths[0].FillColor, fill) {
					t.Fatalf("背景未使用预设: %+v", paths[0].FillColor)
				}
			}
			if !reflect.DeepEqual(paths[len(paths)-1].FillColor, &creator.Color{R: 10, G: 54, B: 34}) {
				t.Fatal("未使用 Bootstrap 5 success 强调色")
			}
		})
	}
	opts := flow.DefaultOptions()
	opts.Theme = flow.CarbonTheme()
	accent, fill, border := &creator.Color{R: 12}, &creator.Color{G: 34}, &creator.Color{B: 56}
	opts.Theme.Callouts.Warning = flow.CalloutPalette{AccentColor: accent, Fill: fill, BorderColor: border}
	d := flow.New(opts)
	d.AddCallout(flow.Callout{Variant: "warning", Appearance: "subtle", Text: "Theme"})
	d.AddCallout(flow.Callout{Variant: "warning", Appearance: "subtle", Text: "Local", AccentColor: border, Fill: accent, BorderColor: fill})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	paths := calloutPaths(model)
	for i, want := range []*creator.Color{fill, border, border, border, accent, accent, fill, fill, fill, border} {
		if !reflect.DeepEqual(paths[i].FillColor, want) {
			t.Fatalf("配色优先级错误: path=%d got=%+v want=%+v", i, paths[i].FillColor, want)
		}
	}
}

func TestCalloutPresetsHaveIndependentPalettes(t *testing.T) {
	seen := map[[3]uint8]bool{}
	for _, theme := range []flow.Theme{flow.MaterialTheme(), flow.Material2Theme(), flow.Material3Theme(), flow.Fluent2Theme(), flow.CarbonTheme(), flow.Bootstrap5Theme()} {
		if theme.Callouts.Info.AccentColor == nil || theme.Callouts.Success.Fill == nil || theme.Callouts.Danger.BorderColor == nil {
			t.Fatal("主题缺少提示框配色")
		}
		c := theme.Callouts.Info.AccentColor
		key := [3]uint8{c.R, c.G, c.B}
		if seen[key] {
			t.Fatal("主题仍共享统一信息色")
		}
		seen[key] = true
	}
}

func TestCalloutStrictKeepRejectsOversizeAndMovesWholeContainer(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddCallout(flow.Callout{Title: "Notice", Pagination: "keep", Text: strings.Repeat("Line\n", 30)})
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "严格同栏") {
		t.Fatalf("严格模式未拒绝超高框: %v", err)
	}
	d = flow.New(smallOptions())
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	d.Blocks = append(d.Blocks, flow.Block{ID: "notice", Kind: "callout", Callout: &flow.Callout{Pagination: "keep", Title: "Notice", Text: "Body"}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bookmarkPages(model)["notice"] != 1 || !strings.Contains(pageText(model.Pages[1]), "Body") {
		t.Fatal("严格模式未整体移栏")
	}
	opts := smallOptions()
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: strings.Repeat("Source\n", 9) + "End"}}
	d = flow.New(opts)
	d.AddCallout(flow.Callout{Pagination: "keep", Title: "Notice", Runs: []flow.Run{{Text: strings.Repeat("Body\n", 4)}, {Footnote: "source"}}})
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "严格同栏") {
		t.Fatalf("严格模式未计入脚注高度: %v", err)
	}
}

func TestCalloutCodeWithoutTitleUsesMonospaceAndShowsIconOnce(t *testing.T) {
	for _, language := range []string{"", "unknown", "go"} {
		t.Run(language, func(t *testing.T) {
			d := flow.New()
			d.Blocks = append(d.Blocks, flow.Block{Kind: "callout", Style: &flow.TextStyle{Font: "Flow-Regular", FirstLineIndent: 5}, Callout: &flow.Callout{ShowIcon: true, Blocks: []flow.Block{{Kind: "code", CodeLang: language, Text: "  value := 1\n\n  println(value)"}}}})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			fixed := map[string]bool{}
			for _, font := range model.Fonts {
				fixed[font.Name] = font.FixedWidth
			}
			icons := 0
			for _, item := range model.Pages[0].Items {
				switch v := item.(type) {
				case creator.Path:
					if v.CTM != nil {
						icons++
					}
				case creator.Text:
					if !fixed[v.Font] {
						t.Fatalf("框内代码未使用等宽字体: %+v", v)
					}
				}
			}
			if icons != 1 || !strings.Contains(pageText(model.Pages[0]), "value") {
				t.Fatalf("图标或代码缺失: icons=%d", icons)
			}
		})
	}
}

// 代码沿用等宽高亮、超长行折行和每个片段一块面板，续标题不能挤掉正文或脚注。
func TestCalloutCodeAndRepeatedTitleAcrossColumns(t *testing.T) {
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 6, Balance: true}
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: "Source"}}
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{ID: "notice", Kind: "callout", Callout: &flow.Callout{
		Variant: "warning", Appearance: "subtle", Title: "Notice", RepeatTitle: true, ContinuationSuffix: " (cont.)", ShowIcon: true,
		Blocks: []flow.Block{
			{Runs: []flow.Run{{Text: "Review first"}, {Footnote: "source"}}},
			{Kind: "code", CodeLang: "go", Text: "// BEGIN\n" + strings.Repeat("fmt.Println(\"abcdefghijklmnopqrstuv\")\n", 25) + "// END"},
			{Text: "After code"},
		},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	icons, panels, repeats := 0, 0, 0
	for _, page := range model.Pages {
		text.WriteString(pageText(page))
		repeats += strings.Count(pageText(page), "(cont.)")
		for _, item := range page.Items {
			switch v := item.(type) {
			case creator.Path:
				if v.CTM != nil {
					icons++
				}
				if reflect.DeepEqual(v.FillColor, &creator.Color{R: 244, G: 245, B: 247}) {
					panels++
				}
				if v.X < 10-0.001 || v.Y < 10-0.001 || v.X+v.Width > 90.001 || v.Y+v.Height > 80.001 {
					t.Fatalf("面板或图标越界: %+v", v)
				}
			case creator.Text:
				if v.X < 10-0.001 || v.Y < 10-0.001 || v.X+v.Width > 90.001 || v.Y+v.Height > 80.001 {
					t.Fatalf("文字越界: %+v", v)
				}
			}
		}
	}
	if icons != 1 || panels < 2 || repeats < 1 {
		t.Fatalf("片段缺失或图标重复: icons=%d panels=%d repeats=%d", icons, panels, repeats)
	}
	for _, value := range []string{"BEGIN", "END", "After code", "Source", "go"} {
		if strings.Count(text.String(), value) != 1 {
			t.Fatalf("内容重复或丢失: %s", value)
		}
	}
	if bookmarkPages(model)["notice"] != 0 {
		t.Fatal("续标题改变了原始目标")
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("多轮排版输出不确定")
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "callout-code.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("严格校验失败: %+v", report.Issues)
	}
}

func TestCalloutRejectsInvalidAppearancePaginationAndContinuation(t *testing.T) {
	for _, c := range []flow.Callout{
		{Text: "Body", Appearance: "unknown"}, {Text: "Body", Pagination: "unknown"},
		{Text: "Body", RepeatTitle: true}, {Text: "Body", Title: "Notice", ContinuationSuffix: " (cont.)"},
		{Text: "Body", Title: "Notice", RepeatTitle: true, ContinuationSuffix: "bad\x00"},
		{Blocks: []flow.Block{{Kind: "code", Runs: []flow.Run{{Text: "bad"}}}}},
	} {
		d := flow.New()
		d.AddCallout(c)
		if _, err := d.Build(context.Background()); err == nil {
			t.Fatalf("未拒绝非法选项: %+v", c)
		}
	}
}
