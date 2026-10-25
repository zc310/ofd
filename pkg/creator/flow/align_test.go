package flow_test

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"golang.org/x/image/font/gofont/goregular"
)

func modelTexts(model *creator.Document) []creator.Text {
	var texts []creator.Text
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				texts = append(texts, text)
			}
		}
	}
	return texts
}

// 段末、显式换行与空行不伸展；首行缩进只占第一行，英文词内推进量保持字体原值。
func TestJustifyStretchesWrappedLinesButPreservesManualBreaksAndWords(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	opts.Theme.Body.FirstLineIndent = 6
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("alpha beta gamma delta ", 12) + "END\r\nManual break\n" + strings.Repeat("one two three ", 12) + "STOP\n\nFinal")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	stretched := 0
	for i, text := range texts {
		value := textValue(text)
		wantX := 10.0
		if i == 0 {
			wantX += 6
		}
		if math.Abs(text.X-wantX) > 0.000001 {
			t.Fatalf("缩进错误: %q x=%g", value, text.X)
		}
		last := strings.HasSuffix(value, "END") || value == "Manual break" || strings.HasSuffix(value, "STOP") || value == "Final"
		if !last && math.Abs(text.X+text.Width-90) > 0.000001 {
			t.Fatalf("自动换行未铺满行宽: %q right=%g", value, text.X+text.Width)
		}
		runes := []rune(value)
		width := fontAdvance(t, goregular.TTF, runes[len(runes)-1]) * text.Size
		for j, advance := range text.TextCodes[0].DeltaX {
			natural := fontAdvance(t, goregular.TTF, runes[j]) * text.Size
			if last || runes[j] != ' ' {
				if math.Abs(advance-natural) > 0.000001 {
					t.Fatalf("词内或段末被伸展: %q rune=%q delta=%g natural=%g", value, runes[j], advance, natural)
				}
			} else if advance > natural+0.000001 {
				stretched++
			}
			width += advance
		}
		if math.Abs(width-text.Width) > 0.000001 {
			t.Fatalf("字符定位与对象宽度不一致: %q width=%g want=%g", value, text.Width, width)
		}
	}
	if stretched < 10 || len(model.Pages) < 2 {
		t.Fatalf("未覆盖跨页伸展: gaps=%d pages=%d", stretched, len(model.Pages))
	}
}

// 字体和链接边界不产生新的词内间距，跨对象的合法间距由下一对象的 X 表达。
func TestJustifyPreservesRichTextLinksAndRunBoundaries(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{
		{Text: "al"}, {Text: "pha", Bold: true}, {Text: " beta "},
		{Text: "gamma delta", Href: "https://example.com"},
		{Text: " epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau.", Strike: true},
	}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	if len(texts) < 5 || textValue(texts[0]) != "al" || strings.TrimSpace(textValue(texts[1])) != "pha" {
		t.Fatalf("富文本被拆分或丢失: %+v", texts)
	}
	if math.Abs(texts[1].X-texts[0].X-texts[0].Width) > 0.000001 {
		t.Fatal("同一单词内的字体边界被伸展")
	}
	linked, shifted := false, false
	for i, text := range texts {
		if len(text.Actions) > 0 && text.Actions[0].URI != nil {
			linked = true
			if text.Actions[0].URI.URI != "https://example.com" {
				t.Fatal("两端对齐丢失链接")
			}
		}
		if i > 0 && text.Y == texts[i-1].Y && text.X > texts[i-1].X+texts[i-1].Width+0.000001 {
			shifted = true
		}
		if text.X < 10 || text.X+text.Width > 90.000001 {
			t.Fatalf("富文本对象越界: %+v", text)
		}
	}
	if !linked || !shifted {
		t.Fatal("未覆盖链接和跨对象间距")
	}
}

func TestJustifyListsCellsFootnotesAndColumnsUseTheirOwnWidths(t *testing.T) {
	opts := smallOptions()
	opts.PageSize = creator.PageSize{Width: 180, Height: 180}
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	opts.Theme.Body.Align = "justify"
	opts.FootnoteStyle = &flow.TextStyle{Align: "justify"}
	opts.Footnotes = []flow.Footnote{{ID: "source", Text: "NOTE source records explain how each measured result was checked and archived for future review."}}
	d := flow.New(opts)
	d.AddList(true, []string{strings.Repeat("list item words ", 8)})
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{Align: "justify"}}, Grid: [][]flow.TableCell{{{Text: strings.Repeat("cell words ", 8)}}}})
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Observed result"}, {Footnote: "source"}}})
	d.AddColumnBreak()
	d.AddParagraph(strings.Repeat("right column words ", 10))
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	list, cell, note, right, marker := false, false, false, false, false
	for _, text := range modelTexts(model) {
		value := textValue(text)
		edge := text.X + text.Width
		if value == "1." {
			marker = true
			if edge > 20 {
				t.Fatal("列表编号被拉宽")
			}
		}
		switch {
		case strings.HasPrefix(value, "list ") && math.Abs(edge-86) < 0.000001:
			list = true
		case strings.HasPrefix(value, "cell ") && math.Abs(edge-84.5) < 0.000001:
			cell = true
		case strings.HasPrefix(value, "NOTE ") && math.Abs(edge-86) < 0.000001:
			note = true
		case strings.HasPrefix(value, "right ") && math.Abs(edge-170) < 0.000001:
			right = true
		}
	}
	if !list || !cell || !note || !right || !marker {
		t.Fatalf("未按各自可用宽度对齐: list=%v cell=%v note=%v right=%v marker=%v", list, cell, note, right, marker)
	}
}

func TestJustifyDoesNotStretchSingleWordsOrNoWrapCells(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	d := flow.New(opts)
	d.AddParagraph("Extraordinary\nShort\nFinal")
	d.AddTable(flow.Table{Columns: []flow.TableColumn{{NoWrap: true, Align: "justify"}}, Rows: [][]string{{"one two\nthree four"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range modelTexts(model) {
		if text.Width >= 70 {
			t.Fatalf("显式换行或不换行单元格被伸展: %q width=%g", textValue(text), text.Width)
		}
	}
	// 只有一个不可分单词的自动换行也保持自然字宽。
	d = flow.New(opts)
	d.Options.PageSize.Width = 50
	d.AddParagraph("Extraordinary Extraordinary Extraordinary")
	model, err = d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range modelTexts(model) {
		if textValue(text) != "Extraordinary" || text.Width >= 30 {
			t.Fatal("单个不可分单词被伸展")
		}
	}
}

// 中文在 UAX #14 字符边界分配间距，仍合并同字体文字并保留每字定位。
func TestJustifyCJKUsesBreakBoundariesAndKeepsTheLastLineNatural(t *testing.T) {
	data, character := fixtureCJKFont(t)
	opts := smallOptions()
	opts.FallbackFonts = []string{"CJK"}
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "CJK", Data: data})
	opts.Theme.Body.Align = "justify"
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat(string(character), 43))
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	if len(texts) < 2 {
		t.Fatal("未产生中文自动换行")
	}
	for i, text := range texts {
		if text.Font != "CJK" || len(text.TextCodes) != 1 {
			t.Fatal("中文回退或合并丢失")
		}
		advance := fontAdvance(t, data, character) * text.Size
		last := i == len(texts)-1
		if !last && math.Abs(text.X+text.Width-90) > 0.000001 {
			t.Fatal("中文自动行未铺满版心")
		}
		for _, delta := range text.TextCodes[0].DeltaX {
			if last && math.Abs(delta-advance) > 0.000001 || !last && delta <= advance {
				t.Fatalf("中文伸展位置错误: last=%v delta=%g natural=%g", last, delta, advance)
			}
		}
	}
}

// 实际栅格的行尾应与固化字符位置一致，防止只拉宽边界却未移动字符。
func TestJustifyRasterUsesExpandedCharacterPositions(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("HH HH HH ", 10) + "END")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	if len(texts) < 3 {
		t.Fatal("未产生足够的自动行")
	}
	var ofd, raster bytes.Buffer
	if err := d.Write(context.Background(), &ofd, creator.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := converter.Convert(context.Background(), "ofd", "png", ofd.Bytes(), &raster, converter.Page(1), converter.DPI(144), converter.BgColor(color.White)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raster.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	scale := 144.0 / 25.4
	for _, text := range []creator.Text{texts[0], texts[len(texts)-1]} {
		right := -1
		for y := int(text.Y * scale); y < int((text.Y+text.Height)*scale); y++ {
			for x := int(text.X * scale); x < int(91*scale); x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r < 0x8000 && g < 0x8000 && b < 0x8000 {
					right = max(right, x)
				}
			}
		}
		edge := text.X + text.Width
		if float64(right)/scale < edge-1.5 || float64(right)/scale > edge+0.5 {
			t.Fatalf("画面行尾未遵循字符位置: %q raster=%g edge=%g", textValue(text), float64(right)/scale, edge)
		}
	}
}
