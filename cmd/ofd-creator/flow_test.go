package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

// YAML 示例及其临时转换的 JSON 均覆盖 --check、生成、严格校验与导出重建。
func TestFlowExamplesCheckCreateAndExport(t *testing.T) {
	for _, name := range []string{"flow-report", "flow-merged-report", "flow-navigation-report", "flow-sections-report", "flow-fonts", "flow-tables-report", "flow-fixed-report", "flow-styles-report", "flow-footnotes-report", "flow-columns-report", "flow-catalogs-report", "flow-justify-report", "flow-fluent2-report", "flow-material2-report", "flow-material3-report", "flow-carbon-report", "flow-bootstrap5-report", "flow-pagination-report", "flow-gbt9704-notice"} {
		t.Run(name, func(t *testing.T) { checkFlowExample(t, name) })
	}
}

func checkFlowExample(t *testing.T, name string) {
	t.Helper()
	var first []byte
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			input := "examples/" + name + ".yaml"
			if format == "json" {
				m, _, err := manifest.Load(input, "yaml")
				if err != nil {
					t.Fatal(err)
				}
				source, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				input = filepath.Join(t.TempDir(), name+".json")
				if err := os.WriteFile(input, source, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			if code := run([]string{"-i", input, "--check"}, &stdout, &stderr); code != 0 {
				t.Fatalf("check=%d: %s", code, stderr.String())
			}
			output := filepath.Join(t.TempDir(), "report.ofd")
			if code := run([]string{"-i", input, "-o", output, "--validate", "--deterministic"}, &stdout, &stderr); code != 0 {
				t.Fatalf("create=%d: %s", code, stderr.String())
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				first = data
			} else if !bytes.Equal(first, data) {
				t.Fatal("JSON/YAML 示例生成结果不一致")
			}
			v, err := validator.New()
			if err != nil {
				t.Fatal(err)
			}
			report := v.ValidateReader(context.Background(), bytes.NewReader(data), "flow.ofd")
			if report.HasErrors() || report.HasWarnings() {
				t.Fatalf("严格校验存在问题: %+v", report.Issues)
			}
			exported := filepath.Join(t.TempDir(), "document."+format)
			if code := run([]string{"export", "-i", output, "-o", exported, "--format", format}, &stdout, &stderr); code != 0 {
				t.Fatalf("export=%d: %s", code, stderr.String())
			}
			m, base, err := manifest.Load(exported, format)
			if err != nil {
				t.Fatal(err)
			}
			if m.Flow != nil || len(m.Pages) < 3 {
				t.Fatalf("导出应保留固定页，页数=%d", len(m.Pages))
			}
			if name == "flow-merged-report" && (len(m.Document.Outlines) != 2 || len(m.Document.Outlines[0].Children) != 2) {
				t.Fatalf("导出丢失标题层级: %+v", m.Document.Outlines)
			}
			if name == "flow-navigation-report" {
				if len(m.Document.Bookmarks) != 7 {
					t.Fatalf("导出丢失内容书签: %+v", m.Document.Bookmarks)
				}
				if len(m.Document.Outlines) != 3 {
					t.Fatalf("导出丢失报告大纲: %+v", m.Document.Outlines)
				}
			}
			if name == "flow-sections-report" {
				if len(m.Document.Bookmarks) != 6 || len(m.Document.Outlines) != 3 {
					t.Fatalf("分节导出丢失导航: bookmarks=%d outlines=%d", len(m.Document.Bookmarks), len(m.Document.Outlines))
				}
				portrait, landscape := 0, 0
				for _, page := range m.Pages {
					if page.Area != nil && page.Area.PhysicalBox != nil {
						if page.Area.PhysicalBox.Width == 220 {
							landscape++
						} else {
							portrait++
						}
					}
				}
				if portrait < 4 || landscape != 1 {
					t.Fatalf("分节导出丢失页面尺寸: portrait=%d landscape=%d", portrait, landscape)
				}
			}
			fixed, err := m.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			if name == "flow-tables-report" {
				if len(fixed.Bookmarks) != 3 || len(fixed.Outlines) != 2 {
					t.Fatalf("导出丢失报告导航: bookmarks=%d outlines=%d", len(fixed.Bookmarks), len(fixed.Outlines))
				}
				continuations, refs := 0, 0
				for _, page := range fixed.Pages {
					items := append([]creator.Item(nil), page.Items...)
					for _, layer := range page.Layers {
						items = append(items, layer.Items...)
					}
					for _, item := range items {
						if text, ok := item.(creator.Text); ok {
							value := text.Value
							for _, code := range text.TextCodes {
								value += code.Value
							}
							if strings.Contains(value, "(continued)") {
								continuations++
							}
							if len(text.Actions) > 0 && text.Actions[0].Goto != nil && strings.HasPrefix(value, "Table ") {
								refs++
							}
						}
					}
				}
				if continuations == 0 || refs < 3 {
					t.Fatalf("导出丢失续表题或编号跳转: continuations=%d refs=%d", continuations, refs)
				}
			}
			if name == "flow-fixed-report" {
				checkFixedExampleExport(t, fixed)
			}
			if name == "flow-styles-report" {
				checkStyleExampleExport(t, fixed)
			}
			if name == "flow-footnotes-report" {
				checkFootnoteExampleExport(t, fixed)
			}
			if name == "flow-columns-report" {
				checkColumnExampleExport(t, fixed)
			}
			if name == "flow-catalogs-report" {
				checkCatalogExampleExport(t, fixed)
			}
			if name == "flow-justify-report" {
				checkJustifyExampleExport(t, fixed)
			}
			if name == "flow-fluent2-report" {
				checkThemeExampleExport(t, fixed, "Fluent 2 report", 24, 10.5, 9, 700, 0x61)
			}
			if name == "flow-material2-report" {
				checkThemeExampleExport(t, fixed, "Material 2", 72, 12, 9, 400, 0x75)
			}
			if name == "flow-material3-report" {
				checkThemeExampleExport(t, fixed, "Material 3 report", 24, 12, 9, 400, 0x49)
			}
			if name == "flow-carbon-report" {
				checkThemeExampleExport(t, fixed, "Carbon report", 31.5, 12, 9, 400, 0x52)
			}
			if name == "flow-bootstrap5-report" {
				checkThemeExampleExport(t, fixed, "Bootstrap 5 report", 30, 12, 10.5, 700, 0x59)
			}
			if name == "flow-gbt9704-notice" {
				checkOfficialExampleExport(t, fixed)
			}
			if code := run([]string{"-i", exported, "--check"}, &stdout, &stderr); code != 0 {
				t.Fatalf("export check=%d: %s", code, stderr.String())
			}
			recreated := filepath.Join(t.TempDir(), "recreated.ofd")
			if code := run([]string{"-i", exported, "-o", recreated, "--validate"}, &stdout, &stderr); code != 0 {
				t.Fatalf("export create=%d: %s", code, stderr.String())
			}
		})
	}
}

func checkOfficialExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	for i, page := range document.Pages {
		if page.Area == nil || page.Area.ContentBox == nil || math.Abs(page.Area.ContentBox.X-28) > 0.0001 || math.Abs(page.Area.ContentBox.Width-156) > 0.0001 || math.Abs(page.Area.ContentBox.Height-225) > 0.0001 {
			t.Fatal("导出丢失公文版心")
		}
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		pageNumber := false
		for _, item := range items {
			if text, ok := item.(creator.Text); ok && len(text.TextCodes) > 0 && strings.HasPrefix(text.TextCodes[0].Value, "— ") {
				pageNumber = true
				want := 28 + 14*25.4/72
				if i%2 == 0 {
					want = 184 - 14*25.4/72 - text.Width
				}
				if math.Abs(text.Size-14*25.4/72) > 0.0001 || math.Abs(text.X-want) > 0.0002 {
					t.Fatal("导出丢失公文页码的奇偶位置或字号")
				}
			}
		}
		if !pageNumber {
			t.Fatal("导出缺少公文页码")
		}
	}
}

func checkThemeExampleExport(t *testing.T, document creator.Document, title string, headingSize, bodySize, captionSize float64, headingWeight int, captionRed uint8) {
	t.Helper()
	heading, body, caption := false, false, false
	for _, page := range document.Pages {
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		for _, item := range items {
			text, ok := item.(creator.Text)
			if !ok || len(text.TextCodes) == 0 {
				continue
			}
			value := strings.Join(strings.Fields(text.TextCodes[0].Value), " ")
			switch {
			case value == title:
				heading = heading || math.Abs(text.Size-headingSize*25.4/72) < 0.0001 && text.Weight == headingWeight
			case strings.HasPrefix(value, "This report uses "):
				body = math.Abs(text.Size-bodySize*25.4/72) < 0.0001
			case value == "Figure 1 Reviewed items":
				caption = math.Abs(text.Size-captionSize*25.4/72) < 0.0001 && math.Abs(text.X-20) < 0.0001 && text.FillColor != nil && text.FillColor.R == captionRed
			}
		}
	}
	if !heading || !body || !caption {
		t.Fatalf("导出丢失主题标题、正文或图注样式: heading=%v body=%v caption=%v", heading, body, caption)
	}
}

// 导出后需保留伸展后的 DeltaX、右栏位置和手动换行的自然宽度。
func checkJustifyExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	full, column, manual := false, false, false
	for _, page := range document.Pages {
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		for _, item := range items {
			text, ok := item.(creator.Text)
			if !ok || len(text.TextCodes) == 0 {
				continue
			}
			value := text.TextCodes[0].Value
			if strings.HasPrefix(value, "This paragraph ") {
				full = math.Abs(text.X-26) < 0.001 && math.Abs(text.X+text.Width-140) < 0.001 && len(text.TextCodes[0].DeltaX) > 10
			}
			if strings.HasPrefix(value, "This topic ") {
				column = math.Abs(text.X-90) < 0.001 && math.Abs(text.X+text.Width-140) < 0.001
			}
			if value == "Final short line." {
				manual = text.Width < 50
			}
		}
	}
	if !full || !column || !manual {
		t.Fatalf("导出丢失两端对齐语义: full=%v column=%v manual=%v", full, column, manual)
	}
}

func checkCatalogExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	anonymous := 0
	for _, bookmark := range document.Bookmarks {
		if strings.HasPrefix(bookmark.Name, "__flow_catalog_") {
			anonymous++
		}
	}
	if anonymous != 2 || len(document.Outlines) != 3 {
		t.Fatalf("导出丢失匿名图表定位，或目录进入大纲: anonymous=%d outlines=%d", anonymous, len(document.Outlines))
	}
	figureTitles, tableTitles, chapterTitles := 0, 0, 0
	for _, page := range document.Pages {
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		for _, item := range items {
			text, ok := item.(creator.Text)
			if !ok || len(text.Actions) == 0 || text.Actions[0].Goto == nil {
				continue
			}
			value := text.Value
			if len(text.TextCodes) > 0 {
				value = ""
				for _, code := range text.TextCodes {
					value += code.Value
				}
			}
			value = strings.TrimSpace(value)
			switch value {
			case "Figure 1 Delivery trend":
				figureTitles++
			case "Table 1 Delivery results":
				tableTitles++
			case "Delivery analysis":
				chapterTitles++
			}
		}
	}
	if figureTitles < 3 || tableTitles < 2 || chapterTitles < 3 {
		t.Fatalf("导出丢失目录或完整标题引用: figures=%d tables=%d chapters=%d", figureTitles, tableTitles, chapterTitles)
	}
}

func checkColumnExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	var review, appendix *creator.GotoAction
	for _, bookmark := range document.Bookmarks {
		destination := bookmark.Goto
		switch bookmark.Name {
		case "review":
			review = &destination
		case "appendix":
			appendix = &destination
		}
	}
	if review == nil || review.Type != "XYZ" || review.Left == nil || *review.Left != 95 || review.Top == nil {
		t.Fatalf("导出丢失右栏导航位置: %+v", review)
	}
	if appendix == nil || appendix.Type != "FitH" || appendix.Left != nil {
		t.Fatalf("导出未恢复单栏导航: %+v", appendix)
	}
	leftNotes, rightNotes, continuation := 0, 0, 0
	for _, page := range document.Pages {
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		for _, item := range items {
			if text, ok := item.(creator.Text); ok {
				value := text.Value
				if len(text.TextCodes) > 0 {
					value = ""
					for _, code := range text.TextCodes {
						value += code.Value
					}
				}
				if strings.Contains(value, "FIELD-NOTE") {
					if text.X < 95 {
						leftNotes++
					} else {
						rightNotes++
					}
				}
				if strings.Contains(value, "(continued)") {
					continuation++
				}
			}
		}
	}
	if leftNotes < 2 || rightNotes < 2 || continuation == 0 {
		t.Fatalf("导出丢失分栏脚注或续表: left=%d right=%d continuation=%d", leftNotes, rightNotes, continuation)
	}
}

func checkFootnoteExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	forward, backward, sourceNotes, estimateNotes := 0, 0, 0, 0
	for pageIndex, page := range document.Pages {
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		pageText := ""
		for _, item := range items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			value := text.Value
			if len(text.TextCodes) > 0 {
				value = ""
				for _, code := range text.TextCodes {
					value += code.Value
				}
			}
			pageText += value + "\n"
			if !strings.HasPrefix(strings.TrimSpace(value), "[") || len(text.Actions) == 0 || text.Actions[0].Goto == nil {
				continue
			}
			target := text.Actions[0].Goto
			if target.Page != pageIndex || target.Top == nil {
				t.Fatalf("导出的脚注跳转失效: %+v", target)
			}
			if *target.Top > text.Y {
				forward++
				// 示例正文为 12pt、表格为 10pt，上标应分别为 9pt 和 7.5pt。
				if text.Size > 12*0.75*25.4/72+0.0001 {
					t.Fatalf("导出丢失正文脚注的上标字号: %+v", text)
				}
			} else {
				backward++
				if math.Abs(text.Size-9*25.4/72) > 0.0001 {
					t.Fatal("导出改变页底编号的正常字号")
				}
			}
		}
		sourceNotes += strings.Count(pageText, "SOURCE-NOTE")
		estimateNotes += strings.Count(pageText, "ESTIMATE-NOTE")
	}
	if sourceNotes < 3 || estimateNotes != 1 || forward < 7 || backward < 5 {
		t.Fatalf("导出丢失脚注或双向链接: source=%d estimate=%d forward=%d backward=%d", sourceNotes, estimateNotes, forward, backward)
	}
}

func checkStyleExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	if len(document.Bookmarks) != 5 || len(document.Outlines) != 3 {
		t.Fatalf("样式报告导出丢失导航: bookmarks=%d outlines=%d", len(document.Bookmarks), len(document.Outlines))
	}
	rows := map[string]creator.Text{}
	for _, page := range document.Pages {
		items := append([]creator.Item(nil), page.Items...)
		for _, layer := range page.Layers {
			items = append(items, layer.Items...)
		}
		for _, item := range items {
			if text, ok := item.(creator.Text); ok {
				value := text.Value
				if len(text.TextCodes) > 0 {
					value = ""
				}
				for _, code := range text.TextCodes {
					value += code.Value
				}
				if strings.HasPrefix(value, "Compact row ") {
					rows[strings.TrimSpace(value)] = text
				}
			}
		}
	}
	one, two := rows["Compact row one"], rows["Compact row two"]
	if len(rows) != 2 || math.Abs(one.X-20) > 0.001 || math.Abs(two.X-20) > 0.001 || math.Abs(two.Y-one.Y-10*25.4/72*1.25) > 0.001 {
		t.Fatalf("导出丢失零缩进或零间距: %+v", rows)
	}
}

func checkFixedExampleExport(t *testing.T, document creator.Document) {
	t.Helper()
	if len(document.Bookmarks) != 4 || len(document.Outlines) != 3 {
		t.Fatalf("固定块导出丢失导航: bookmarks=%d outlines=%d", len(document.Bookmarks), len(document.Outlines))
	}
	blocks, images, composites, references := 0, 0, 0, 0
	var walk func([]creator.Item)
	walk = func(items []creator.Item) {
		for _, item := range items {
			switch v := item.(type) {
			case creator.PageBlock:
				blocks++
				walk(v.Items)
			case creator.Path:
				if _, err := models.ParsePathData(v.Data); err != nil {
					t.Fatalf("固定块路径不符合 OFD 语法: %v", err)
				}
			case creator.Image:
				if v.ResourceID != 0 {
					images++
				}
			case creator.Composite:
				if v.ResourceID != 0 {
					composites++
				}
			case creator.Text:
				if len(v.Actions) > 0 && v.Actions[0].Goto != nil {
					references++
				}
			}
		}
	}
	for _, page := range document.Pages {
		walk(page.Items)
		for _, layer := range page.Layers {
			walk(layer.Items)
		}
	}
	for _, unit := range document.Composites {
		walk(unit.Items)
	}
	if blocks < 4 || images != 2 || composites != 2 || references < 5 {
		t.Fatalf("导出丢失固定内容: blocks=%d images=%d composites=%d references=%d", blocks, images, composites, references)
	}
}
