package manifest

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
	"golang.org/x/image/font/gofont/goregular"
)

func TestFlowManifestLoadsFontsAndRejectsConflictingContent(t *testing.T) {
	m := Manifest{Version: 1, Document: Document{ID: "flow-test", Author: "Author"},
		Resources: Resources{Fonts: []Font{{Name: "Custom", DataBase64: base64.StdEncoding.EncodeToString(goregular.TTF)}}},
		Flow:      &Flow{Font: "Custom", Blocks: []FlowBlock{{Type: "heading", Level: 1, Text: "Title"}, {Text: "Paragraph"}}}}
	doc, err := m.Build("", "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Author != "Author" || len(doc.Pages) != 1 {
		t.Fatalf("元数据或页面丢失: %+v", doc)
	}
	m.Pages = []Page{{}}
	if _, err := m.Build("", ""); err == nil || !strings.Contains(err.Error(), "不能同时") {
		t.Fatalf("未拒绝 flow/pages 冲突: %v", err)
	}
	m.Pages = nil
	m.Flow.Blocks = []FlowBlock{{Type: "image", Image: &FlowImage{File: "../outside.png"}}}
	if _, err := m.Build(t.TempDir(), ""); err == nil {
		t.Fatal("未拒绝图片路径穿越")
	}
	m.Flow.Blocks = []FlowBlock{{Type: "image", Image: &FlowImage{File: "image.png", DataBase64: "AA=="}}}
	if _, err := m.Build(t.TempDir(), ""); err == nil {
		t.Fatal("未拒绝图片 file/data_base64 冲突")
	}
}

func TestFlowManifestRejectsUnknownFieldsInBothFormats(t *testing.T) {
	for _, test := range []struct{ ext, source string }{
		{"yaml", "version: 1\nflow:\n  blocks:\n    - text: Hello\n      misspelled: true\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"text":"Hello","misspelled":true}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks:\n    - type: table\n      table:\n        grid:\n          - [{text: Hello, unknown_span: 2}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"table","table":{"grid":[[{"text":"Hello","unknown_span":2}]]}}]}}`},
		{"yaml", "version: 1\nflow:\n  font_families: [{regular: Flow-Regular, unknown_style: Flow-Bold}]\n"},
		{"json", `{"version":1,"flow":{"font_families":[{"regular":"Flow-Regular","unknown_style":"Flow-Bold"}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks:\n    - type: table\n      table:\n        columns: [{unknown_width: 3}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"table","table":{"columns":[{"unknown_width":3}]}}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks:\n    - type: table\n      table:\n        grid: [[{text: X, appearance: {borders: {top: {unknown_width: 1}}}}]]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"table","table":{"grid":[[{"text":"X","appearance":{"borders":{"top":{"unknown_width":1}}}}]]}}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks:\n    - type: fixed\n      fixed: {height: 10, unknown_size: 20}\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"fixed","fixed":{"height":10,"unknown_size":20}}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks:\n    - type: fixed\n      fixed:\n        height: 10\n        items: [{type: path, unknown_data: M}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"fixed","fixed":{"height":10,"items":[{"type":"path","unknown_data":"M"}]}}]}}`},
		{"yaml", "version: 1\nflow:\n  styles: [{name: compact, style: {unknown_spacing: 0}}]\n"},
		{"json", `{"version":1,"flow":{"styles":[{"name":"compact","style":{"unknown_spacing":0}}]}}`},
		{"yaml", "version: 1\nflow:\n  footnotes: [{id: source, text: Source, unknown_note: true}]\n"},
		{"json", `{"version":1,"flow":{"footnotes":[{"id":"source","text":"Source","unknown_note":true}]}}`},
		{"yaml", "version: 1\nflow:\n  columns: {count: 2, unknown_gap: 8}\n"},
		{"json", `{"version":1,"flow":{"columns":{"count":2,"unknown_gap":8}}}`},
		{"yaml", "version: 1\nflow:\n  blocks: [{type: section, section: {columns: {count: 2, unknown_gap: 8}}}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"section","section":{"columns":{"count":2,"unknown_gap":8}}}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks: [{type: toc, toc: {source: figures, unknown_catalog: true}}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"type":"toc","toc":{"source":"figures","unknown_catalog":true}}]}}`},
		{"yaml", "version: 1\nflow:\n  profile: gbt9704\n  official: {fonts: {unknown_font: X}}\n"},
		{"json", `{"version":1,"flow":{"profile":"gbt9704","official":{"fonts":{"unknown_font":"X"}}}}`},
		{"yaml", "version: 1\nflow:\n  profile: gbt9704\n  official: {fonts: {}, letterhead: {org: Office, unknown_mark: X}}\n"},
		{"json", `{"version":1,"flow":{"profile":"gbt9704","official":{"fonts":{},"letterhead":{"org":"Office","unknown_mark":"X"}}}}`},
	} {
		t.Run(test.ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input."+test.ext)
			if err := os.WriteFile(path, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Load(path, "auto"); err == nil {
				t.Fatal("未拒绝流式内容中的未知字段")
			}
		})
	}
}

func TestOfficialProfileBuildsFromBothFormatsAndRejectsConflicts(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			m := Manifest{Version: 1, Document: Document{ID: "official"}, Flow: &Flow{Profile: "gbt9704", Official: &FlowOfficial{
				Fonts:      FlowOfficialFonts{Body: "Flow-Regular", Title: "Flow-Regular", Hei: "Flow-Bold", Kai: "Flow-Italic", Song: "Flow-Regular"},
				Letterhead: &FlowOfficialLetterhead{Org: "EXAMPLE OFFICE", DocNo: "OF [2026] 1"},
				Signature:  &FlowOfficialSignature{Org: "Office", Date: "2026-10-25"},
				Colophon:   &FlowOfficialColophon{IssuedBy: "Office Secretariat"},
			}, Blocks: []FlowBlock{{Type: "heading", Level: 1, Text: "Notice"}, {Type: "recipient", Text: "All departments:"}, {Text: "Please review and archive all delivery records."}}}}
			var data []byte
			var err error
			if format == "json" {
				data, err = json.Marshal(m)
			} else {
				data, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), "official."+format)
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, base, err := Load(input, format)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := loaded.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) != 1 || doc.Pages[0].Area.ContentBox.X != 28 || doc.Pages[0].Area.ContentBox.Width != 156 {
				t.Fatal("清单未采用公文几何")
			}
			pageNumber, recipient := false, false
			for _, item := range doc.Pages[0].Items {
				if text, ok := item.(creator.Text); ok {
					for _, code := range text.TextCodes {
						if code.Value == "— 1 —" {
							pageNumber = true
						}
						if code.Value == "All departments:" && text.X == 28 {
							recipient = true
						}
					}
				}
			}
			if !pageNumber || !recipient {
				t.Fatal("公文页码或主送机关丢失")
			}
			loaded.Flow.Theme = "material3"
			if _, err := loaded.Build(base, ""); err == nil || !strings.Contains(err.Error(), "互斥") {
				t.Fatalf("主题冲突未拒绝: %v", err)
			}
			loaded.Flow.Theme = ""
			loaded.Flow.Profile = ""
			if _, err := loaded.Build(base, ""); err == nil || !strings.Contains(err.Error(), "profile") {
				t.Fatalf("未拒绝缺失 profile: %v", err)
			}
			loaded.Flow.Profile = "gbt9704"
			loaded.Flow.Official.Fonts.Kai = "missing"
			if _, err := loaded.Build(base, ""); err == nil || !strings.Contains(err.Error(), "未注册") {
				t.Fatalf("未拒绝字体缺失: %v", err)
			}
		})
	}
}

func TestNamedStylesAndExplicitZeroSurviveManifestSerialization(t *testing.T) {
	for _, test := range []struct{ format, source string }{
		{"yaml", "version: 1\nflow:\n  styles:\n    - {name: base, style: {first_line_indent: 6, space_before: 2, space_after: 4}}\n    - {name: flat, style: {based_on: base, first_line_indent: 0, space_before: 0, space_after: 0}}\n  blocks:\n    - {text: Inherited, style: {based_on: base}}\n    - {text: Flat, style: {based_on: flat}}\n    - {text: 'Null', style: {based_on: base, first_line_indent: null}}\n"},
		{"json", `{"version":1,"flow":{"styles":[{"name":"base","style":{"first_line_indent":6,"space_before":2,"space_after":4}},{"name":"flat","style":{"based_on":"base","first_line_indent":0,"space_before":0,"space_after":0}}],"blocks":[{"text":"Inherited","style":{"based_on":"base"}},{"text":"Flat","style":{"based_on":"flat"}},{"text":"Null","style":{"based_on":"base","first_line_indent":null}}]}}`},
	} {
		t.Run(test.format, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "styles."+test.format)
			if err := os.WriteFile(input, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				m, base, err := Load(input, "auto")
				if err != nil {
					t.Fatal(err)
				}
				flat := m.Flow.Styles[1].Style
				if flat.FirstLineIndent == nil || *flat.FirstLineIndent != 0 || flat.SpaceBefore == nil || flat.SpaceAfter == nil || m.Flow.Blocks[2].Style.FirstLineIndent != nil {
					t.Fatal("解析丢失显式零值或 null 继承")
				}
				doc, err := m.Build(base, "")
				if err != nil {
					t.Fatal(err)
				}
				texts := make([]creator.Text, 0)
				for _, item := range doc.Pages[0].Items {
					if text, ok := item.(creator.Text); ok {
						texts = append(texts, text)
					}
				}
				if len(texts) != 3 || texts[0].X != 26 || texts[1].X != 20 || texts[2].X != 26 {
					t.Fatalf("命名样式的零缩进未应用: %+v", texts)
				}
				lineHeight := 12 * 25.4 / 72 * 1.6
				if math.Abs(texts[1].Y-texts[0].Y-lineHeight-4) > 0.001 || math.Abs(texts[2].Y-texts[1].Y-lineHeight-2) > 0.001 {
					t.Fatal("显式零段间距未应用")
				}
				var encoded []byte
				if test.format == "json" {
					encoded, err = json.Marshal(m)
				} else {
					encoded, err = yaml.Marshal(m)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(input, encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestManifestRejectsExplicitZeroSizeAndLineHeight(t *testing.T) {
	for _, test := range []struct{ format, source string }{
		{"yaml", "version: 1\nflow:\n  blocks: [{text: Invalid, style: {size: 0}}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"text":"Invalid","style":{"size":0}}]}}`},
		{"yaml", "version: 1\nflow:\n  blocks: [{text: Invalid, style: {line_height: 0}}]\n"},
		{"json", `{"version":1,"flow":{"blocks":[{"text":"Invalid","style":{"line_height":0}}]}}`},
	} {
		input := filepath.Join(t.TempDir(), "invalid."+test.format)
		if err := os.WriteFile(input, []byte(test.source), 0600); err != nil {
			t.Fatal(err)
		}
		m, base, err := Load(input, "auto")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Build(base, ""); err == nil || !strings.Contains(err.Error(), "blocks[0].style") || !strings.Contains(err.Error(), "字号和行高") {
			t.Fatalf("未拒绝显式零字号/行高: %v", err)
		}
	}
}

func TestFlowFontFaceSelectionLoadsInBothManifestFormats(t *testing.T) {
	collection := make([]byte, 20+len(goregular.TTF))
	copy(collection, "ttcf")
	be := binary.BigEndian
	be.PutUint32(collection[4:], 0x00010000)
	be.PutUint32(collection[8:], 2)
	be.PutUint32(collection[12:], 20)
	be.PutUint32(collection[16:], 20)
	copy(collection[20:], goregular.TTF)
	for i := 0; i < int(be.Uint16(goregular.TTF[4:])); i++ {
		offset := 20 + 12 + i*16 + 8
		be.PutUint32(collection[offset:], be.Uint32(collection[offset:])+20)
	}
	for _, test := range []struct{ format, source string }{
		{"yaml", "version: 1\ndocument: {id: selected}\nresources:\n  fonts: [{name: Custom, file: pair.ttc}]\nflow:\n  font: Custom\n  font_faces: {Custom: 1}\n  font_families: [{regular: Custom}]\n  blocks:\n    - runs: [{text: Keep custom, bold: true}]\n"},
		{"json", `{"version":1,"document":{"id":"selected"},"resources":{"fonts":[{"name":"Custom","file":"pair.ttc"}]},"flow":{"font":"Custom","font_faces":{"Custom":1},"font_families":[{"regular":"Custom"}],"blocks":[{"runs":[{"text":"Keep custom","bold":true}]}]}}`},
	} {
		t.Run(test.format, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "pair.ttc"), collection, 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "document."+test.format)
			if err := os.WriteFile(path, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			m, base, err := Load(path, "auto")
			if err != nil {
				t.Fatal(err)
			}
			doc, err := m.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, font := range doc.Fonts {
				if font.Name == "Custom" {
					found = true
					if bytes.HasPrefix(font.Data, []byte("ttcf")) || font.Source != nil {
						t.Fatal("清单未应用字体面选择")
					}
				}
			}
			if !found {
				t.Fatal("清单字体丢失")
			}
		})
	}
}

// 两种清单都应在字体和局部样式覆盖之前应用主题，并保留 material 默认行为。
func TestFlowThemeSelectionAndOverridesInBothFormats(t *testing.T) {
	for _, theme := range []string{"fluent2", "material2", "material3", "carbon", "bootstrap5"} {
		for _, format := range []string{"json", "yaml"} {
			t.Run(theme+"/"+format, func(t *testing.T) {
				m := Manifest{Version: 1, Document: Document{ID: "themes"}, Flow: &Flow{Theme: theme, Blocks: []FlowBlock{
					{Type: "heading", Level: 1, Text: "Heading"},
					{Text: "Body one\nBody two"},
					{Type: "table", Table: &FlowTable{Rows: [][]string{{"Cell"}}}},
				}}}
				build := func() creator.Document {
					t.Helper()
					var data []byte
					var err error
					if format == "json" {
						data, err = json.Marshal(m)
					} else {
						data, err = yaml.Marshal(m)
					}
					if err != nil {
						t.Fatal(err)
					}
					input := filepath.Join(t.TempDir(), "themes."+format)
					if err := os.WriteFile(input, data, 0600); err != nil {
						t.Fatal(err)
					}
					loaded, base, err := Load(input, format)
					if err != nil {
						t.Fatal(err)
					}
					doc, err := loaded.Build(base, "")
					if err != nil {
						t.Fatal(err)
					}
					return doc
				}
				texts := func(doc creator.Document) map[string]creator.Text {
					result := make(map[string]creator.Text)
					for _, page := range doc.Pages {
						for _, item := range page.Items {
							if text, ok := item.(creator.Text); ok {
								value := text.Value
								for _, code := range text.TextCodes {
									value += code.Value
								}
								result[value] = text
							}
						}
					}
					return result
				}
				got := texts(build())
				bodySize, bodyLine, headingFont := 10.5, 15.0, "Flow-Bold"
				headingSize := 24.0
				tableSize := 10.5
				if theme == "material2" || theme == "material3" {
					bodySize, bodyLine, headingFont = 12, 18, "Flow-Regular"
				}
				if theme == "material2" {
					headingSize = 72
				}
				if theme == "carbon" {
					headingSize, bodySize, bodyLine, headingFont = 31.5, 12, 18, "Flow-Regular"
				}
				if theme == "bootstrap5" {
					headingSize, bodySize, bodyLine, tableSize = 30, 12, 18, 12
				}
				if math.Abs(got["Heading"].Size-headingSize*25.4/72) > 0.000001 || got["Heading"].Font != headingFont || math.Abs(got["Body one"].Size-bodySize*25.4/72) > 0.000001 || math.Abs(got["Body two"].Y-got["Body one"].Y-bodyLine*25.4/72) > 0.000001 || math.Abs(got["Cell"].Size-tableSize*25.4/72) > 0.000001 {
					t.Fatal("清单没有应用主题的标题、正文、行距和表格字号")
				}
				m.Resources.Fonts = []Font{{Name: "Custom", DataBase64: base64.StdEncoding.EncodeToString(goregular.TTF)}}
				m.Flow.Font, m.Flow.HeadingFont = "Custom", "Custom"
				m.Flow.BodySize, m.Flow.LineHeight = 13, 1.7
				localSize := 14.0
				m.Flow.Styles = []FlowNamedStyle{{Name: "local", Style: FlowStyle{Size: &localSize}}}
				m.Flow.Blocks = append(m.Flow.Blocks, FlowBlock{Text: "Override", Style: &FlowStyle{BasedOn: "local"}})
				got = texts(build())
				if got["Heading"].Font != "Custom" || got["Body one"].Font != "Custom" || math.Abs(got["Heading"].Size-headingSize*25.4/72) > 0.000001 || math.Abs(got["Body one"].Size-13*25.4/72) > 0.000001 || math.Abs(got["Body two"].Y-got["Body one"].Y-13*1.7*25.4/72) > 0.000001 || math.Abs(got["Override"].Size-14*25.4/72) > 0.000001 {
					t.Fatal("主题选择覆盖了显式字体、正文字号、行高或命名样式")
				}
				m.Flow.Theme = "material"
				explicit := build()
				m.Flow.Theme = ""
				omitted := build()
				if !reflect.DeepEqual(explicit.Pages, omitted.Pages) {
					t.Fatal("默认主题与显式 material 不一致")
				}
				m.Flow.Theme = "unknown"
				if _, err := m.Build("", ""); err == nil || !strings.Contains(err.Error(), "未知主题") {
					t.Fatalf("未知主题未被拒绝: %v", err)
				}
			})
		}
	}
}

// 省略/null 沿用所选版式的默认值，false 必须在 JSON/YAML 再序列化后仍保留。
func TestFlowPageNumbersRespectExplicitFalseAndProfileDefaults(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, profile := range []string{"", "gbt9704"} {
			for _, value := range []string{"omitted", "null", "false", "true"} {
				t.Run(format+"/"+profile+"/"+value, func(t *testing.T) {
					m := Manifest{Version: 1, Document: Document{ID: "page-numbers"}, Flow: &Flow{Profile: profile, Blocks: []FlowBlock{{Text: "Body"}}}}
					if profile != "" {
						m.Flow.Official = &FlowOfficial{Fonts: FlowOfficialFonts{Body: "Flow-Regular", Title: "Flow-Regular", Hei: "Flow-Bold", Kai: "Flow-Italic", Song: "Flow-Regular"}}
					}
					if value == "false" || value == "true" {
						v := value == "true"
						m.Flow.PageNumbers = &v
					}
					var data []byte
					var err error
					if format == "json" {
						data, err = json.Marshal(m)
					} else {
						data, err = yaml.Marshal(m)
					}
					if err != nil {
						t.Fatal(err)
					}
					if value == "null" {
						if format == "json" {
							data = bytes.Replace(data, []byte(`"flow":{`), []byte(`"flow":{"page_numbers":null,`), 1)
						} else {
							data = bytes.Replace(data, []byte("flow:\n"), []byte("flow:\n    page_numbers: null\n"), 1)
						}
					}
					for pass := 0; pass < 2; pass++ {
						input := filepath.Join(t.TempDir(), "numbers."+format)
						if err := os.WriteFile(input, data, 0600); err != nil {
							t.Fatal(err)
						}
						loaded, base, err := Load(input, format)
						if err != nil {
							t.Fatal(err)
						}
						if (value == "false" || value == "true") && (loaded.Flow.PageNumbers == nil || *loaded.Flow.PageNumbers != (value == "true")) {
							t.Fatal("显式页码开关丢失")
						}
						if (value == "omitted" || value == "null") && loaded.Flow.PageNumbers != nil {
							t.Fatal("省略或 null 未保留默认语义")
						}
						doc, err := loaded.Build(base, "")
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for _, page := range doc.Pages {
							for _, item := range page.Items {
								if text, ok := item.(creator.Text); ok {
									for _, code := range text.TextCodes {
										if code.Value == "— 1 —" || code.Value == "1 / 1" {
											found = true
										}
									}
								}
							}
						}
						want := value == "true" || (value == "omitted" || value == "null") && profile == "gbt9704"
						if found != want {
							t.Fatalf("页码显示=%v want=%v", found, want)
						}
						if format == "json" {
							data, err = json.Marshal(loaded)
						} else {
							data, err = yaml.Marshal(loaded)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
