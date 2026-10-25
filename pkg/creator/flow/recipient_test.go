package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 用真实 Go 字体轮廓补充测试标签，避免迁移回归依赖本机公文字体。
func recipientTestOptions(t *testing.T) flow.Options {
	t.Helper()
	opts := officialTestOptions()
	face, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	g, err := face.GlyphIndex(&buffer, 'H')
	if err != nil {
		t.Fatal(err)
	}
	var mappings []fontrepair.GlyphMapping
	for _, r := range "主送抄送：。，H" {
		mappings = append(mappings, fontrepair.GlyphMapping{Rune: r, Glyph: uint16(g)})
	}
	data, err := fontrepair.RepairWithGlyphs(goregular.TTF, mappings)
	if err != nil {
		t.Fatal(err)
	}
	opts.Fonts[0].Data = data
	return opts
}

func recipientNames(n int) string {
	var names []string
	for i := 0; i < n; i++ {
		names = append(names, fmt.Sprintf("DEPT%02d", i))
	}
	return strings.Join(names, "\n") + "："
}

func recipientDocument(t *testing.T, n int, head, colophon bool) *flow.Document {
	t.Helper()
	opts := recipientTestOptions(t)
	if head {
		opts.Official.Letterhead = &flow.OfficialLetterhead{Org: "OFFICE", DocNo: "OF [2026] 1"}
	}
	if colophon {
		opts.Official.Colophon = &flow.OfficialColophon{Cc: "COPY-OFFICE.", IssuedBy: "SECRETARIAT"}
	}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{ID: "title", Kind: "heading", Level: 1, Text: "Notice"}, {ID: "recipients", Kind: "recipient", Text: recipientNames(n)}, {ID: "body", Text: "Body first line\nBody second line"}}
	return d
}

func TestLongRecipientMovesToColophonAndKeepsBodyOnFirstPage(t *testing.T) {
	for _, head := range []bool{false, true} {
		for _, colophon := range []bool{false, true} {
			t.Run(fmt.Sprintf("head=%v/colophon=%v", head, colophon), func(t *testing.T) {
				d := recipientDocument(t, 24, head, colophon)
				original := d.Blocks[1]
				originalColophon := d.Options.Official.Colophon
				model, err := d.Build(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				pages := bookmarkPages(model)
				wantPages := 1
				if head {
					wantPages = 2
				}
				if pages["title"] != 0 || pages["body"] != 0 || pages["recipients"] != len(model.Pages)-1 || len(model.Pages) != wantPages {
					t.Fatalf("主送没有迁移到末页或首页正文丢失: %+v", pages)
				}
				if head && strings.Contains(pageText(model.Pages[0]), "DEPT") || !strings.Contains(pageText(model.Pages[0]), "Body first line") {
					t.Fatal("迁移后首页仍有长主送或没有正文")
				}
				last := model.Pages[len(model.Pages)-1]
				text := pageText(last)
				if strings.Count(text, "主送：") != 1 || !strings.Contains(text, "DEPT23。") {
					t.Fatalf("主送标签或句号错误: %q", text)
				}
				for i := 0; i < 24; i++ {
					if strings.Count(text, fmt.Sprintf("DEPT%02d", i)) != 1 {
						t.Fatal("主送机关丢失或重复")
					}
				}
				if colophon && !(strings.Index(text, "主送：") < strings.Index(text, "抄送：") && strings.Index(text, "抄送：") < strings.Index(text, "SECRETARIAT")) {
					t.Fatal("版记主送、抄送与印发机关顺序错误")
				}
				var primaryBottom, copyTop float64
				bottomLine := false
				for _, item := range last.Items {
					switch v := item.(type) {
					case creator.Text:
						if strings.HasPrefix(textValue(v), "DEPT") {
							if math.Abs(v.Size-14*25.4/72) > 0.000001 || v.X < 28+14*25.4/72 {
								t.Fatal("主送未采用四号字和一字内边距")
							}
							primaryBottom = math.Max(primaryBottom, v.Y+v.Height)
						}
						if textValue(v) == "抄送：" {
							copyTop = v.Y
						}
					case creator.Path:
						if v.Height == 0.35 && math.Abs(v.Y+v.Height-262) < 0.000001 {
							bottomLine = true
						}
					}
				}
				if !bottomLine {
					t.Fatal("迁移改变版记底边位置")
				}
				if colophon {
					for _, item := range last.Items {
						if p, ok := item.(creator.Path); ok && p.Y >= primaryBottom && p.Y < copyTop {
							t.Fatal("主送与抄送之间出现分隔线")
						}
					}
				}
				if !reflect.DeepEqual(d.Blocks[1], original) || d.Options.Official.Colophon != originalColophon {
					t.Fatal("迁移修改了调用方内容或版记配置")
				}
			})
		}
	}
}

func TestShortRecipientStaysBeforeBody(t *testing.T) {
	d := recipientDocument(t, 2, true, true)
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	if pages["recipients"] != 0 || pages["body"] != 0 || strings.Contains(pageText(model.Pages[0]), "主送：") {
		t.Fatal("能排下的短主送被错误迁移")
	}
}

func TestRelocatedRecipientKeepsReferencesLinksAndSingleFontSnapshot(t *testing.T) {
	d := recipientDocument(t, 24, true, true)
	source := &countedFontSource{data: d.Options.Fonts[0].Data}
	d.Options.Fonts[0].Data = nil
	d.Options.Fonts[0].Source = source
	d.Blocks[1].Text = ""
	d.Blocks[1].Runs = []flow.Run{{Text: strings.TrimSuffix(recipientNames(23), "：") + "\n"}, {Text: "LAST-OFFICE", Href: "https://example.com"}, {Text: " body page "}, {PageRef: "body"}, {Text: "："}}
	d.Blocks[2].Text = ""
	d.Blocks[2].Runs = []flow.Run{{Text: "Body first line; recipient page "}, {PageRef: "recipients"}, {Text: "\nBody second line"}}
	original := append([]flow.Run(nil), d.Blocks[1].Runs...)
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source.opens != 1 {
		t.Fatalf("迁移或页码回填重复读取字体: %d", source.opens)
	}
	pages := bookmarkPages(model)
	if pages["body"] != 0 || pages["recipients"] != 1 {
		t.Fatalf("迁移目的地错误: %v", pages)
	}
	linked, forward, back := false, false, false
	for pageIndex, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				for _, action := range text.Actions {
					if action.URI != nil && action.URI.URI == "https://example.com" && strings.HasPrefix(textValue(text), "LAST-OFFICE") {
						linked = true
					}
					if action.Goto != nil {
						if pageIndex == 0 && action.Goto.Page == 1 && strings.TrimSpace(textValue(text)) == "2" {
							forward = true
						}
						if pageIndex == 1 && action.Goto.Page == 0 && strings.TrimSpace(textValue(text)) == "1" {
							back = true
						}
					}
				}
			}
		}
	}
	if !linked || !forward || !back || !reflect.DeepEqual(original, d.Blocks[1].Runs) {
		t.Fatal("迁移丢失链接、最终页码引用或修改了输入")
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("迁移输出不确定")
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "recipient.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("迁移输出严格校验失败: %+v", report.Issues)
	}
}

func TestRecipientRelocationDoesNotOverrideExplicitBreaksOrImpossibleColophon(t *testing.T) {
	for _, boundary := range []string{"page_break", "column_break", "section", "before", "after"} {
		t.Run(boundary, func(t *testing.T) {
			d := recipientDocument(t, 24, true, false)
			switch boundary {
			case "before":
				d.Blocks[2].PageBreakBefore = true
			case "after":
				d.Blocks[1].PageBreakAfter = true
			case "section":
				d.Blocks = append(d.Blocks[:2], append([]flow.Block{{Kind: "section", Section: &flow.Section{}}}, d.Blocks[2:]...)...)
			default:
				d.Blocks = append(d.Blocks[:2], append([]flow.Block{{Kind: boundary}}, d.Blocks[2:]...)...)
			}
			if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "首页必须") {
				t.Fatalf("迁移覆盖了显式边界: %v", err)
			}
		})
	}
	d := recipientDocument(t, 40, true, false)
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "版记超过") {
		t.Fatalf("超高版记未报错: %v", err)
	}
}

func TestMultipleRecipientBlocksRelocateInOrderWithoutRepeatedLabels(t *testing.T) {
	d := recipientDocument(t, 12, true, true)
	second := flow.Block{ID: "other", Kind: "recipient", Text: strings.ReplaceAll(recipientNames(12), "DEPT", "OTHER")}
	d.Blocks = append(d.Blocks[:2], append([]flow.Block{second}, d.Blocks[2:]...)...)
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pages := bookmarkPages(model)
	last := pageText(model.Pages[len(model.Pages)-1])
	if pages["body"] != 0 || pages["recipients"] != 1 || pages["other"] != 1 || strings.Count(last, "主送：") != 1 || strings.Index(last, "DEPT00") >= strings.Index(last, "OTHER00") || !strings.Contains(last, "DEPT11，") || !strings.Contains(last, "OTHER11。") {
		t.Fatalf("多块主送迁移顺序或标签错误: pages=%v text=%q", pages, last)
	}
}

func TestRelocatedRecipientRejectsFootnotesAndReportsUnresolvedFirstPage(t *testing.T) {
	d := recipientDocument(t, 24, true, false)
	d.Options.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE"}}
	d.Blocks[1].Text = ""
	d.Blocks[1].Runs = []flow.Run{{Text: recipientNames(24)}, {Footnote: "note"}}
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "不能包含脚注") {
		t.Fatalf("版记迁移静默丢弃或误排脚注: %v", err)
	}
	d = recipientDocument(t, 24, true, false)
	d.Blocks[0].Text = strings.Repeat("Title\n", 12)
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "移至版记后首页仍无法") {
		t.Fatalf("迁移后首页仍没有正文未报明确错误: %v", err)
	}
}
