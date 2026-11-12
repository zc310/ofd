package markdown

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func alertText(block flow.Block) string {
	var text strings.Builder
	text.WriteString(block.Text)
	for _, run := range block.Runs {
		text.WriteString(run.Text)
	}
	return text.String()
}

func TestMarkdownAlertsRecognizeFiveGitHubTypes(t *testing.T) {
	for _, tc := range []struct {
		kind, title string
		color       creator.Color
	}{
		{"NOTE", "Note", creator.Color{R: 9, G: 105, B: 218}},
		{"TIP", "Tip", creator.Color{R: 26, G: 127, B: 55}},
		{"IMPORTANT", "Important", creator.Color{R: 130, G: 80, B: 223}},
		{"WARNING", "Warning", creator.Color{R: 154, G: 103, B: 0}},
		{"CAUTION", "Caution", creator.Color{R: 207, G: 34, B: 46}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			for _, newline := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll("> [!"+tc.kind+"]\n> Body **bold** and [link](https://example.com).\n", "\n", newline)
				doc, err := parseMarkdown([]byte(source), "", flow.DefaultOptions())
				if err != nil {
					t.Fatal(err)
				}
				if len(doc.Blocks) != 2 || alertText(doc.Blocks[0]) != tc.title || !doc.Blocks[0].KeepWithNext || !doc.Blocks[0].Runs[0].Bold {
					t.Fatalf("提示标题或正文丢失: %+v", doc.Blocks)
				}
				for _, block := range doc.Blocks {
					if block.LeftBorderColor == nil || !reflect.DeepEqual(*block.LeftBorderColor, tc.color) || strings.Contains(alertText(block), "[!") {
						t.Fatalf("提示标记或色条不正确: %+v", block)
					}
				}
				if !strings.HasPrefix(alertText(doc.Blocks[1]), "Body bold and link.") {
					t.Fatalf("首行后的正文丢失: %+v", doc.Blocks[1])
				}
				bold, link := false, false
				for _, run := range doc.Blocks[1].Runs {
					bold = bold || run.Bold && run.Text == "bold"
					link = link || run.Href == "https://example.com"
				}
				if !bold || !link {
					t.Fatal("提示正文行内样式或链接丢失")
				}
				model, err := doc.Build(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				titleColor := false
				for _, item := range model.Pages[0].Items {
					if text, ok := item.(creator.Text); ok && text.FillColor != nil && reflect.DeepEqual(*text.FillColor, tc.color) {
						titleColor = true
					}
				}
				if !titleColor {
					t.Fatal("提示标题没有使用对应颜色")
				}
			}
		})
	}
}

func TestMarkdownAlertKeepsMultipleBlocksAndSeparatesFollowingContent(t *testing.T) {
	source := "> [!NOTE]\n>\n> First paragraph.\n>\n> - item\n>   - nested\n>\n> ```go\n>     x := 1\n> ```\n>\n> | A | B |\n> | --- | --- |\n> | one | two |\n>\n> Last paragraph.\n\nOutside.\n"
	doc, err := parseMarkdown([]byte(source), "", flow.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"paragraph", "paragraph", "list_item", "list_item", "code", "table", "paragraph", "paragraph"}
	if len(doc.Blocks) != len(want) {
		t.Fatalf("块数不正确: %+v", doc.Blocks)
	}
	for i, kind := range want {
		b := doc.Blocks[i]
		if b.Kind != kind || (b.LeftBorderColor != nil) != (i < len(want)-1) {
			t.Fatalf("blocks[%d] 丢失类型或越过提示边界: %+v", i, b)
		}
	}
	if doc.Blocks[3].Indent != 6 || doc.Blocks[4].Text != "    x := 1" {
		t.Fatal("嵌套列表或代码缩进丢失")
	}
	model, err := doc.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 1 {
		t.Fatal("短提示出现多余分页")
	}
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := doc.Write(t.Context(), output, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("提示排版输出不确定")
	}
}

func TestMarkdownAlertMarkersOnlyRecognizedOnDedicatedFirstQuoteLine(t *testing.T) {
	for _, source := range []string{
		"> [!UNKNOWN]\n> Body\n",
		"> [!note]\n> Body\n",
		"> [!NOTE] custom title\n> Body\n",
		"> ordinary\n> [!NOTE]\n> Body\n",
		"> `[!NOTE]`\n> Body\n",
		"> \\[!NOTE]\n> Body\n",
		"[!NOTE]\n\nBody\n",
		"```\n> [!NOTE]\n> Body\n```\n",
	} {
		t.Run(source, func(t *testing.T) {
			doc, err := parseMarkdown([]byte(source), "")
			if err != nil {
				t.Fatal(err)
			}
			for _, block := range doc.Blocks {
				if block.LeftBorderColor != nil {
					t.Fatalf("普通文本被识别为提示: %+v", block)
				}
			}
		})
	}
}

func TestMarkdownAlertEmptyAndNestedQuotePreserveContent(t *testing.T) {
	for _, source := range []string{"> [!TIP]\n", "> [!TIP]\n>\n", "> > [!TIP]\n> > Body\n"} {
		doc, err := parseMarkdown([]byte(source), "", flow.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		if alertText(doc.Blocks[0]) != "Tip" {
			t.Fatalf("提示标题丢失: %+v", doc.Blocks)
		}
		if _, err := doc.Build(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkdownAlertMarkerLineBreakAndReferenceDoNotConsumeBody(t *testing.T) {
	for _, source := range []string{
		"> [!NOTE]  \n> Body **bold**.\n",
		"> [!NOTE]\n> Body.\n\n[!NOTE]: https://example.com\n",
		"> [!NOTE]\n> `code` and text.\n",
	} {
		doc, err := parseMarkdown([]byte(source), "", flow.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Blocks) != 2 || alertText(doc.Blocks[1]) == "" {
			t.Fatalf("标记行吞掉了正文: %q / %+v", source, doc.Blocks)
		}
		if _, err := doc.Build(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

// 提示块可跨页，标题不孤悬，正文和表格、代码的续页色条都不得丢失或越界。
func TestMarkdownAlertsKeepBordersAcrossPagesAndColumns(t *testing.T) {
	for _, kind := range []string{"paragraph", "code", "table"} {
		t.Run(kind, func(t *testing.T) {
			opts := flow.DefaultOptions()
			opts.PageSize = creator.PageSize{Width: 160, Height: 80}
			opts.MarginTop, opts.MarginBottom, opts.MarginLeft, opts.MarginRight = 8, 8, 8, 8
			opts.Columns = flow.Columns{Count: 2, Gap: 6}
			body := strings.Repeat("> Line.  \n", 60)
			if kind == "code" {
				body = "> ```go\n" + strings.Repeat("> x()\n", 60) + "> ```\n"
			}
			if kind == "table" {
				body = "> | A | B |\n> | --- | --- |\n" + strings.Repeat("> | one | two |\n", 60)
			}
			doc, err := parseMarkdown([]byte("> [!WARNING]\n"+body), "", opts)
			if err != nil {
				t.Fatal(err)
			}
			model, err := doc.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Pages) < 2 {
				t.Fatal("长提示未跨页")
			}
			for pageIndex, page := range model.Pages {
				borders := 0
				for _, item := range page.Items {
					if path, ok := item.(creator.Path); ok && path.FillColor != nil && reflect.DeepEqual(path.FillColor, doc.Blocks[0].LeftBorderColor) {
						borders++
						if path.X < 8 || path.X+path.Width > 152 || path.Y < 7.999 || path.Y+path.Height > 72.001 {
							t.Fatalf("第 %d 页色条越界: %+v", pageIndex+1, path)
						}
					}
				}
				if borders == 0 {
					t.Fatalf("第 %d 页缺少续页色条", pageIndex+1)
				}
			}
			if len(model.Pages[0].Items) <= 2 {
				t.Fatal("提示标题孤悬")
			}
		})
	}
}

func TestMarkdownAlertTitleFollowsBodyOntoNextPage(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.PageSize = creator.PageSize{Width: 100, Height: 60}
	opts.MarginTop, opts.MarginBottom, opts.MarginLeft, opts.MarginRight = 10, 10, 10, 10
	source := "First prefix.  \nSecond prefix.  \nThird prefix.\n\n> [!NOTE]\n> Body line.  \n> Last line.\n"
	doc, err := parseMarkdown([]byte(source), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 {
		t.Fatalf("页数错误: %d", len(model.Pages))
	}
	for pageIndex, page := range model.Pages {
		var text strings.Builder
		for _, item := range page.Items {
			if value, ok := item.(creator.Text); ok {
				for _, code := range value.TextCodes {
					text.WriteString(code.Value)
				}
			}
		}
		if pageIndex == 0 && strings.Contains(text.String(), "Note") {
			t.Fatal("提示标题孤悬前页")
		}
		if pageIndex == 1 && (!strings.Contains(text.String(), "Note") || !strings.Contains(text.String(), "Body line.")) {
			t.Fatalf("提示标题没有与正文同页: %s", text.String())
		}
	}
}
