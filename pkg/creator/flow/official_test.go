package flow_test

import (
	"bytes"
	"context"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 测试字体用于核对结构和几何，不把 Go 字体声明为仿宋、小标宋、黑体、楷体或宋体。
func officialTestOptions() flow.Options {
	return flow.GBT9704Options(flow.OfficialFonts{Body: "Flow-Regular", Title: "Flow-Regular", Hei: "Flow-Bold", Kai: "Flow-Italic", Song: "Flow-Regular"})
}

func glyphInkEdges(t *testing.T, text creator.Text, data []byte) (float64, float64) {
	t.Helper()
	f, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	upper, lower := 0.0, 0.0
	for _, r := range textValue(text) {
		g, err := f.GlyphIndex(&buf, r)
		if err != nil {
			t.Fatal(err)
		}
		bounds, _, err := f.GlyphBounds(&buf, g, 64000, font.HintingNone)
		if err != nil {
			t.Fatal(err)
		}
		upper = math.Min(upper, float64(bounds.Min.Y)/64000*text.Size)
		lower = math.Max(lower, float64(bounds.Max.Y)/64000*text.Size)
	}
	return textBaseline(text) + upper, textBaseline(text) + lower
}

func TestOfficialProfileProducesPageGeometryElementsAndOddEvenNumbers(t *testing.T) {
	opts := officialTestOptions()
	opts.GenerateOutlines = true
	opts.Official.Letterhead = &flow.OfficialLetterhead{Org: "EXAMPLE OFFICE", DocNo: "OF [2026] 1", SerialNo: "000001", Urgency: "URGENT"}
	opts.Official.Signature = &flow.OfficialSignature{Org: "Example Office", Date: "2026-10-25"}
	opts.Official.Colophon = &flow.OfficialColophon{IssuedBy: "Example Office Secretariat"}
	d := flow.New(opts)
	d.AddHeading(1, "Administrative notice")
	d.AddRecipient("All departments:")
	d.Blocks = append(d.Blocks, flow.Block{ID: "body", Text: "Body paragraph starts here."})
	d.AddPageBreak()
	d.AddHeading(2, "1. Requirements")
	d.AddParagraph("Review the records and preserve the final document.")
	d.AddPageBreak()
	d.AddHeading(3, "2. Details")
	d.AddParagraph("The original data and final output are archived together.")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 3 {
		t.Fatalf("公文页数=%d", len(model.Pages))
	}
	var org, docNo, title, recipient, body, signature, date creator.Text
	var red creator.Path
	for p, page := range model.Pages {
		box := page.Area.ContentBox
		if page.Area.PhysicalBox.Width != 210 || page.Area.PhysicalBox.Height != 297 || box.X != 28 || box.Y != 37 || box.Width != 156 || box.Height != 225 {
			t.Fatalf("公文版心错误: %+v", page.Area)
		}
		pageNumber := false
		for _, item := range page.Items {
			switch v := item.(type) {
			case creator.Text:
				value := textValue(v)
				switch value {
				case "EXAMPLE OFFICE":
					org = v
					if p != 0 {
						t.Fatal("版头出现在续页")
					}
				case "OF [2026] 1":
					docNo = v
				case "Administrative notice":
					title = v
				case "All departments:":
					recipient = v
				case "Body paragraph starts here.":
					body = v
				case "Example Office":
					signature = v
				case "2026-10-25":
					date = v
				case "1. Requirements":
					if v.Font != "Flow-Bold" {
						t.Fatal("第一层结构字体错误")
					}
				case "2. Details":
					if v.Font != "Flow-Italic" {
						t.Fatal("第二层结构字体错误")
					}
				}
				if strings.HasPrefix(value, "— ") {
					pageNumber = true
					want := 28 + 14*25.4/72
					if p%2 == 0 {
						want = 184 - 14*25.4/72 - v.Width
					}
					top, _ := glyphInkEdges(t, creator.Text{Y: v.Y, Size: v.Size, TextCodes: []creator.TextCode{{Value: "—", Y: v.TextCodes[0].Y}}}, goregular.TTF)
					if math.Abs(v.X-want) > 0.000001 || math.Abs(v.Size-14*25.4/72) > 0.000001 || math.Abs(top-269) > 0.000001 {
						t.Fatalf("页码位置错误: %+v inkTop=%g", v, top)
					}
				}
			case creator.Path:
				if v.FillColor != nil && v.FillColor.R == 204 && v.FillColor.G == 0 && v.Height == 0.5 {
					red = v
				}
			}
		}
		if !pageNumber {
			t.Fatal("缺少公文专用页码")
		}
	}
	if org.Size == 0 || title.Size == 0 || body.Size == 0 || signature.Size == 0 || date.Size == 0 {
		t.Fatal("公文要素丢失")
	}
	orgTop, _ := glyphInkEdges(t, org, goregular.TTF)
	_, docBottom := glyphInkEdges(t, docNo, goregular.TTF)
	if math.Abs(orgTop-72) > 0.000001 || math.Abs(red.X-28) > 0.000001 || red.Width != 156 || math.Abs(red.Y-docBottom-4) > 0.000001 {
		t.Fatal("机关标志或红线的位置不符合标准条款")
	}
	if math.Abs(title.Size-22*25.4/72) > 0.000001 || math.Abs(title.X+title.Width/2-106) > 0.000001 || recipient.X != 28 || math.Abs(body.X-28-2*156.0/28) > 0.000001 {
		t.Fatal("标题、主送机关或两字缩进错误")
	}
	last := model.Pages[2]
	bottomLine := false
	for _, item := range last.Items {
		if p, ok := item.(creator.Path); ok && p.Height == 0.35 && math.Abs(p.Y+p.Height-262) < 0.000001 {
			bottomLine = true
		}
	}
	if !bottomLine {
		t.Fatal("末条版记线未压准版心底边")
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("公文输出不确定")
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "official.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("公文 OFD 严格校验失败: %+v", report.Issues)
	}
}

func TestOfficialGridFitsTwentyEightFullwidthCharactersAndTwentyTwoLines(t *testing.T) {
	data, ch := fixtureCJKFont(t)
	if math.Abs(fontAdvance(t, data, ch)-1) > 0.000001 {
		t.Fatal("用于网格回归的汉字不是全角字宽")
	}
	fonts := flow.OfficialFonts{Body: "CJK", Title: "Flow-Regular", Hei: "Flow-Bold", Kai: "Flow-Italic", Song: "Flow-Regular"}
	opts := flow.GBT9704Options(fonts)
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "CJK", Data: data})
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Text: strings.Repeat(string(ch), 28*45), Style: &flow.TextStyle{Align: "left", Set: flow.StyleFirstLineIndent}, Orphans: 1, Widows: 1}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 3 {
		t.Fatalf("45 行应为 22+22+1: %d 页", len(model.Pages))
	}
	for p, page := range model.Pages {
		count, previousY := 0, 0.0
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok || text.Font != "CJK" {
				continue
			}
			if len([]rune(textValue(text))) != 28 || math.Abs(text.Width-156) > 0.000001 || math.Abs(text.X-28) > 0.000001 || math.Abs(text.HScale-156.0/(28*16*25.4/72)) > 0.000001 {
				t.Fatal("28 字网格未撑满版心")
			}
			if count > 0 && math.Abs(text.Y-previousY-225.0/22) > 0.000001 {
				t.Fatal("正文未采用 22 行行距")
			}
			previousY = text.Y
			count++
		}
		want := 22
		if p == 2 {
			want = 1
		}
		if count != want {
			t.Fatalf("第 %d 页行数=%d want=%d", p+1, count, want)
		}
	}
}

func TestOfficialProfileRejectsMissingFontsAndUnsupportedGeometry(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*flow.Options)
		want   string
	}{
		{"missing font", func(o *flow.Options) { o.Official.Fonts.Kai = "missing" }, "未注册"},
		{"empty font", func(o *flow.Options) { o.Official.Fonts.Song = "" }, "必须指定"},
		{"margins", func(o *flow.Options) { o.MarginTop = 20 }, "版心"},
		{"columns", func(o *flow.Options) { o.Columns = flow.Columns{Count: 2, Gap: 8} }, "单栏"},
		{"header", func(o *flow.Options) { o.Header = "Header" }, "header/footer"},
		{"wide organization", func(o *flow.Options) { o.Official.Letterhead = &flow.OfficialLetterhead{Org: strings.Repeat("W", 100)} }, "单行要素"},
		{"control character", func(o *flow.Options) { o.Official.Signature = &flow.OfficialSignature{Org: "bad\x00"} }, "控制字符"},
		{"wide signature", func(o *flow.Options) { o.Official.Signature = &flow.OfficialSignature{Org: strings.Repeat("W", 100)} }, "落款"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := officialTestOptions()
			test.change(&opts)
			d := flow.New(opts)
			d.AddParagraph("Body")
			if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v want=%s", err, test.want)
			}
		})
	}
}

func TestOfficialProfileRequiresBodyOnFirstPageAndKeepsLinksBlack(t *testing.T) {
	opts := officialTestOptions()
	opts.Official.Letterhead = &flow.OfficialLetterhead{Org: "OFFICE", DocNo: "OF [2026] 1"}
	d := flow.New(opts)
	d.AddHeading(1, "Notice")
	d.AddPageBreak()
	d.AddParagraph("Body")
	if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "首页必须") {
		t.Fatalf("首页没有正文仍成功: %v", err)
	}
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Body", Href: "https://example.com"}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range modelTexts(model) {
		if textValue(text) == "Body" && (len(text.Actions) != 1 || text.FillColor == nil || text.FillColor.R != 0 || text.FillColor.G != 0 || text.FillColor.B != 0) {
			t.Fatal("公文链接没有保留黑色正文与点击动作")
		}
	}
}

func TestOfficialColophonDoesNotOverlapFootnotes(t *testing.T) {
	opts := officialTestOptions()
	opts.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE"}}
	opts.Official.Colophon = &flow.OfficialColophon{IssuedBy: "Office Secretariat"}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Body"}, {Footnote: "note"}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 || !strings.Contains(pageText(model.Pages[0]), "NOTE") || strings.Contains(pageText(model.Pages[0]), "Office Secretariat") || !strings.Contains(pageText(model.Pages[1]), "Office Secretariat") {
		t.Fatal("版记未避开末页脚注")
	}
	for _, item := range model.Pages[1].Items {
		if p, ok := item.(creator.Path); ok && p.Height == 0.35 && p.Y+p.Height > 262.000001 {
			t.Fatal("版记越出版心下边缘")
		}
	}
}

// 使用真实 Go 字形度量构造混合字体，中文标签映射到已有轮廓以免依赖本机字体。
// 带下伸的斜体姓名与发文字号应共享基线，红线以整行可见下缘为准。
func TestOfficialSignatorySharesBaselineAcrossDifferentFontMetrics(t *testing.T) {
	face, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	glyph, err := face.GlyphIndex(&buffer, 'H')
	if err != nil {
		t.Fatal(err)
	}
	var mappings []fontrepair.GlyphMapping
	for _, r := range "签发人：" {
		mappings = append(mappings, fontrepair.GlyphMapping{Rune: r, Glyph: uint16(glyph)})
	}
	data, err := fontrepair.RepairWithGlyphs(goregular.TTF, mappings)
	if err != nil {
		t.Fatal(err)
	}
	opts := officialTestOptions()
	opts.Official.Fonts.Body = "BodyTest"
	opts.Theme.Body.Font = "BodyTest"
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "BodyTest", Data: data})
	opts.Official.Letterhead = &flow.OfficialLetterhead{Org: "OFFICE", DocNo: "OF [2026] 1", Signatory: "gj"}
	d := flow.New(opts)
	d.AddHeading(1, "Notice")
	d.AddParagraph("Body")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]creator.Text{}
	var red creator.Path
	for _, item := range model.Pages[0].Items {
		switch v := item.(type) {
		case creator.Text:
			parts[textValue(v)] = v
		case creator.Path:
			if v.Height == 0.5 && v.FillColor != nil && v.FillColor.R == 204 {
				red = v
			}
		}
	}
	doc, prefix, name := parts["OF [2026] 1"], parts["签发人："], parts["gj"]
	if doc.Size == 0 || prefix.Size == 0 || name.Size == 0 {
		t.Fatal("并排要素丢失")
	}
	if math.Abs(textBaseline(doc)-textBaseline(prefix)) > 0.000001 || math.Abs(textBaseline(doc)-textBaseline(name)) > 0.000001 {
		t.Fatal("签发人不同字体未共享基线")
	}
	_, docBottom := glyphInkEdges(t, doc, data)
	_, prefixBottom := glyphInkEdges(t, prefix, data)
	_, nameBottom := glyphInkEdges(t, name, goitalic.TTF)
	if nameBottom <= docBottom {
		t.Fatal("测试未覆盖姓名下伸超出发文字号")
	}
	if math.Abs(red.Y-math.Max(docBottom, math.Max(prefixBottom, nameBottom))-4) > 0.000001 {
		t.Fatal("红线未按整行最低可见字形留出 4mm")
	}
	if math.Abs(doc.X-28-156.0/28) > 0.000001 || math.Abs(name.X+name.Width-184+156.0/28) > 0.000001 {
		t.Fatal("基线修正改变了一字水平留白")
	}
}

// 开头的分节与换页标记先应用设置，版头在第一次实际内容前生成一次。
// 已有正文之后的分节继续换页，后续不会重复版头。
func TestOfficialLeadingSectionsApplyBeforeLetterheadAndPreserveLaterBreaks(t *testing.T) {
	build := func(prefix []flow.Block) *creator.Document {
		t.Helper()
		opts := officialTestOptions()
		opts.GenerateOutlines = true
		opts.Official.Letterhead = &flow.OfficialLetterhead{Org: "OFFICE", DocNo: "OF [2026] 1"}
		d := flow.New(opts)
		d.Blocks = append(d.Blocks, prefix...)
		d.Blocks = append(d.Blocks, flow.Block{ID: "notice", Kind: "heading", Level: 1, Text: "Notice"}, flow.Block{Kind: "recipient", Text: "All departments:"}, flow.Block{Text: "Body"})
		d.AddSection(flow.Section{})
		d.AddParagraph("Following page")
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return model
	}
	plain := build(nil)
	for _, prefix := range [][]flow.Block{
		{{Kind: "section", Section: &flow.Section{}}},
		{{Kind: "section", Section: &flow.Section{}}, {Kind: "section", Section: &flow.Section{}}},
		{{Kind: "page_break"}, {Kind: "section", Section: &flow.Section{}}, {Kind: "column_break"}},
	} {
		model := build(prefix)
		if len(model.Pages) != 2 || !reflect.DeepEqual(model.Pages, plain.Pages) || !reflect.DeepEqual(model.Bookmarks, plain.Bookmarks) || !reflect.DeepEqual(model.Outlines, plain.Outlines) {
			t.Fatal("开头标记改变版头、分页或导航")
		}
		if strings.Count(pageText(model.Pages[0]), "OFFICE") != 1 || strings.Contains(pageText(model.Pages[1]), "OFFICE") {
			t.Fatal("版头缺失或重复")
		}
	}
	hide, show := false, true
	model := build([]flow.Block{{Kind: "section", Section: &flow.Section{PageNumbers: &hide}}})
	for _, page := range model.Pages {
		if strings.Contains(pageText(page), "— ") {
			t.Fatal("开头分节关闭页码未生效")
		}
	}
	model = build([]flow.Block{{Kind: "section", Section: &flow.Section{PageNumbers: &hide}}, {Kind: "section", Section: &flow.Section{PageNumbers: &show}}})
	if !reflect.DeepEqual(model.Pages, plain.Pages) {
		t.Fatal("连续开头分节没有采用最后生效的页码设置")
	}
}
