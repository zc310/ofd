package markdown

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/spec"
)

func TestAlertSVGIconsConvertToDistinctStandardVectorPaths(t *testing.T) {
	icons, err := loadAlertIcons()
	if err != nil {
		t.Fatal(err)
	}
	if len(icons) != 5 {
		t.Fatalf("内置图标数不正确: %d", len(icons))
	}
	seen := map[string]bool{}
	for title, icon := range icons {
		if icon.Width != 16 || icon.Height != 16 || icon.Rule != "Even-Odd" || seen[icon.Data] {
			t.Fatalf("图标视口、填充规则或类型不正确: %s / %+v", title, icon)
		}
		seen[icon.Data] = true
		if _, err := spec.ParsePathData(icon.Data); err != nil {
			t.Fatalf("%s 图标不是合法 OFD 路径: %v", title, err)
		}
	}
	if !strings.Contains(icons["Tip"].Data, " B ") || strings.Contains(icons["Tip"].Data, " Z") {
		t.Fatal("SVG 三次曲线或闭合命令没有转换为 OFD B/C")
	}
}

func TestAlertIconSourceRejectsUnsupportedSVGConventions(t *testing.T) {
	for _, source := range []string{
		`<svg`,
		`<svg viewBox="0 0 24 24"><path fill-rule="evenodd" d="M 0 0 L 1 1 Z"/></svg>`,
		`<svg viewBox="0 0 16 16"><path fill-rule="evenodd" d="m 0 0 l 1 1 z"/></svg>`,
		`<svg viewBox="0 0 16 16"><path fill-rule="evenodd" d=""/></svg>`,
		`<svg viewBox="0 0 16 16"><path fill-rule="evenodd" d="M 0 0 B 1 1 Z"/></svg>`,
	} {
		if _, err := parseAlertIcon([]byte(source)); err == nil {
			t.Fatalf("不受支持的内置 SVG 未报错: %s", source)
		}
	}
}

func TestMarkdownAlertIconPrecedesTitleAndDoesNotRepeatOnContinuationPages(t *testing.T) {
	for _, kind := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
		t.Run(kind, func(t *testing.T) {
			opts := flow.DefaultOptions()
			opts.PageSize = creator.PageSize{Width: 100, Height: 60}
			opts.MarginTop, opts.MarginBottom, opts.MarginLeft, opts.MarginRight = 10, 10, 10, 10
			doc, err := parseMarkdown([]byte("> [!"+kind+"]\n"+strings.Repeat("> Body line.  \n", 20)), "", opts)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Blocks[0].LeadingIcon == nil || doc.Blocks[1].LeadingIcon != nil {
				t.Fatal("图标应只附在提示标题上")
			}
			model, err := doc.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Pages) < 2 {
				t.Fatal("未覆盖续页")
			}
			count := 0
			var icon creator.Path
			for pageIndex, page := range model.Pages {
				for _, item := range page.Items {
					if path, ok := item.(creator.Path); ok && path.Data == doc.Blocks[0].LeadingIcon.Data {
						count++
						icon = path
						if pageIndex != 0 || path.FillColor != doc.Blocks[0].Style.Color || path.CTM == nil {
							t.Fatal("图标颜色、缩放或分页不正确")
						}
					}
				}
			}
			if count != 1 {
				t.Fatalf("图标应只绘制一次，实际 %d", count)
			}
			foundTitle := false
			for _, item := range model.Pages[0].Items {
				if text, ok := item.(creator.Text); ok && text.Font != "" {
					for _, code := range text.TextCodes {
						if code.Value == doc.Blocks[0].Runs[0].Text {
							foundTitle = true
							if text.X <= icon.X+icon.Width {
								t.Fatalf("图标与标题重叠: %+v / %+v", icon, text)
							}
						}
					}
				}
			}
			if !foundTitle {
				t.Fatal("添加图标后丢失可提取的标题文字")
			}
			var output bytes.Buffer
			if err := doc.Write(t.Context(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
