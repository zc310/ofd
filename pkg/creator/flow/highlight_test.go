package flow_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func TestCodeHighlightPreservesPanelPaginationAndContinuationColors(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Height = 60
	source := "/* start\n" + strings.Repeat("    still a comment\n", 30) + "*/\nreturn 42"
	var plainPanels []creator.Path
	for _, language := range []string{"unknown-ofd-language", "go"} {
		doc := flow.New(opts)
		doc.Blocks = []flow.Block{{Kind: "code", Text: source, CodeLang: language, Orphans: 1, Widows: 1}}
		model, err := doc.Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(model.Pages) < 3 {
			t.Fatal("未覆盖代码跨页")
		}
		var panels []creator.Path
		comments := 0
		for _, page := range model.Pages {
			for _, item := range page.Items {
				switch value := item.(type) {
				case creator.Path:
					panels = append(panels, value)
				case creator.Text:
					if strings.Contains(textValue(value), "still a comment") {
						comments++
						if c := value.FillColor; c == nil || c.R != 0x57 || c.G != 0x60 || c.B != 0x6a {
							t.Fatalf("续页注释没有保持灰色: %+v", c)
						}
					}
				}
			}
		}
		if comments != 30 || len(panels) != len(model.Pages) {
			t.Fatalf("续页丢失注释或重复绘制背景: comments=%d panels=%d", comments, len(panels))
		}
		if language != "go" {
			plainPanels = panels
			continue
		}
		if len(panels) != len(plainPanels) {
			t.Fatal("高亮改变了分页数量")
		}
		for i, panel := range panels {
			plain := plainPanels[i]
			if panel.X != plain.X || panel.Y != plain.Y || panel.Width != plain.Width || panel.Height != plain.Height {
				t.Fatalf("高亮改变了第 %d 页面板: %+v / %+v", i, panel, plain)
			}
		}
		var first, second bytes.Buffer
		for _, output := range []*bytes.Buffer{&first, &second} {
			if err := doc.Write(t.Context(), output, creator.CreateOptions{Deterministic: true}); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("高亮代码输出不确定")
		}
	}
}

func TestLongCodeLineWrapsAcrossPagesWithoutOverflowOrLosingHighlight(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Height = 60
	doc := flow.New(opts)
	doc.Blocks = []flow.Block{{Kind: "code", CodeLang: "go", Text: "var s = \"" + strings.Repeat("a", 1000) + "\"\nreturn 42", Orphans: 1, Widows: 1}}
	model, err := doc.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 2 {
		t.Fatal("未覆盖单行长代码跨页面折行")
	}
	labels := 0
	var restored strings.Builder
	for _, page := range model.Pages {
		var panel creator.Path
		panels := 0
		for _, item := range page.Items {
			if value, ok := item.(creator.Path); ok {
				panel = value
				panels++
			}
		}
		if panels != 1 {
			t.Fatalf("折行后每页应只有一个代码面板: %d", panels)
		}
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			if value := textValue(text); value == "go" {
				labels++
			} else {
				restored.WriteString(value)
				if strings.Contains(value, "aaa") {
					if c := text.FillColor; c == nil || c.R != 0x0a || c.G != 0x30 || c.B != 0x69 {
						t.Fatalf("折行后的字符串丢失高亮: %+v", c)
					}
				}
			}
			if text.X < panel.X+2-0.001 || text.X+text.Width > panel.X+panel.Width-2+0.001 ||
				text.Y < panel.Y || text.Y+text.Height > panel.Y+panel.Height+0.001 {
				t.Fatalf("长代码折行后越出面板: %+v / %+v", text, panel)
			}
		}
	}
	if labels != 1 || restored.String() != "var s = \""+strings.Repeat("a", 1000)+"\"return 42" {
		t.Fatalf("折行丢失文字或重复语言标签: labels=%d / %q", labels, restored.String())
	}
}
