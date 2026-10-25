package flow_test

import (
	"bytes"
	"context"
	"fmt"
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

func textBaseline(text creator.Text) float64 {
	return text.Y + *text.TextCodes[0].Y
}

// 正文编号使用小字号上标，页底编号仍与注释齐平；常规行距和正文基线保持稳定。
func TestFootnoteSuperscriptsPreserveBodyBaselinesAndBottomNumberAlignment(t *testing.T) {
	for _, kind := range []string{"paragraph", "list_item", "table"} {
		for _, columns := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/columns=%v", kind, columns), func(t *testing.T) {
				build := func(reference bool) *creator.Document {
					t.Helper()
					opts := smallOptions()
					opts.PageSize = creator.PageSize{Width: 180, Height: 180}
					opts.Theme.Body.Size = 16
					if columns {
						opts.Columns = flow.Columns{Count: 2, Gap: 8}
					}
					opts.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE"}}
					d := flow.New(opts)
					d.AddParagraph("Before")
					runs := []flow.Run{{Text: "Body"}}
					if reference {
						runs = append(runs, flow.Run{Footnote: "note"})
					}
					runs = append(runs, flow.Run{Text: "."})
					block := flow.Block{Kind: kind, Runs: runs, Style: &flow.TextStyle{Size: 16, LineHeight: 1.6}}
					if kind == "table" {
						block.Runs = nil
						block.Table = &flow.Table{Grid: [][]flow.TableCell{{{Runs: runs}}}}
					}
					d.Blocks = append(d.Blocks, block)
					d.AddParagraph("After")
					model, err := d.Build(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					return model
				}
				got, plain := build(true), build(false)
				var body, source, bottom, note, after, plainBody, plainAfter creator.Text
				for _, text := range modelTexts(got) {
					switch strings.TrimSpace(textValue(text)) {
					case "Body":
						body = text
					case "After":
						after = text
					case "NOTE":
						note = text
					case "[1]":
						if source.Size == 0 {
							source = text
						} else {
							bottom = text
						}
					}
				}
				for _, text := range modelTexts(plain) {
					switch textValue(text) {
					case "Body.":
						plainBody = text
					case "After":
						plainAfter = text
					}
				}
				if body.Size == 0 || source.Size == 0 || bottom.Size == 0 || note.Size == 0 || plainBody.Size == 0 {
					t.Fatal("正文、上标或页底编号丢失")
				}
				if math.Abs(source.Size-body.Size*0.75) > 0.000001 || math.Abs(textBaseline(body)-textBaseline(source)-body.Size*0.3) > 0.000001 || source.Y >= body.Y {
					t.Fatalf("上标字号或基线错误: body=%+v source=%+v", body, source)
				}
				if math.Abs(source.Width-naturalWidth(t, "[1]", goregular.TTF, source.Size)) > 0.000001 || math.Abs(source.Height-body.Height*0.75) > 0.000001 {
					t.Fatal("上标的文字边界和测量字号不一致")
				}
				if math.Abs(textBaseline(body)-textBaseline(plainBody)) > 0.000001 || math.Abs(after.Y-plainAfter.Y) > 0.000001 {
					t.Fatal("正常行距下的正文基线或后续内容位置被上标改变")
				}
				if math.Abs(bottom.Size-note.Size) > 0.000001 || math.Abs(textBaseline(bottom)-textBaseline(note)) > 0.000001 {
					t.Fatal("页底编号没有与注释正文齐平")
				}
				if len(source.Actions) != 1 || source.Actions[0].Goto == nil || source.Actions[0].Goto.Top == nil || *source.Actions[0].Goto.Top <= source.Y {
					t.Fatal("上标未跳转至页底脚注")
				}
				if len(bottom.Actions) != 1 || bottom.Actions[0].Goto == nil || bottom.Actions[0].Goto.Top == nil || math.Abs(*bottom.Actions[0].Goto.Top-source.Y) > 0.000001 {
					t.Fatal("页底编号未返回上标的实际位置")
				}
			})
		}
	}
}

// 紧凑行距只补足实际需要的高度，栏顶上标不能越出顶部或覆盖相邻行。
func TestFootnoteSuperscriptsFitTightLinesAtColumnTops(t *testing.T) {
	opts := smallOptions()
	opts.PageSize = creator.PageSize{Width: 180, Height: 180}
	opts.Theme.Body.LineHeight = 1
	opts.Columns = flow.Columns{Count: 2, Gap: 8}
	opts.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE"}}
	d := flow.New(opts)
	for column := 0; column < 2; column++ {
		if column > 0 {
			d.AddColumnBreak()
		}
		d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Body"}, {Footnote: "note"}, {Text: "\nNext"}, {Footnote: "note"}}})
	}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	markers := 0
	var previousBody creator.Text
	for _, text := range modelTexts(model) {
		if text.Y < 10-0.000001 || text.Y+text.Height > 170.000001 {
			t.Fatalf("上标或脚注越出版心: %+v", text)
		}
		switch strings.TrimSpace(textValue(text)) {
		case "Body":
			previousBody = text
		case "Next":
			if text.Y < previousBody.Y+previousBody.Height-0.000001 {
				t.Fatal("紧凑行距下的相邻正文行重叠")
			}
		case "[1]":
			if text.Y < 100 {
				markers++
				if math.Abs(text.Size-12*0.75*25.4/72) > 0.000001 {
					t.Fatal("紧凑行距下的上标字号错误")
				}
			}
		}
	}
	if len(model.Pages) != 1 || markers != 4 {
		t.Fatalf("分栏上标或分页错误: pages=%d markers=%d", len(model.Pages), markers)
	}
}

// 断行必须使用缩小后的编号宽度，否则原本能放进一行的引用会误报越界。
func TestFootnoteSuperscriptWidthParticipatesInWrapping(t *testing.T) {
	opts := smallOptions()
	size := opts.Theme.Body.Size * 25.4 / 72
	width := naturalWidth(t, "Body.", goregular.TTF, size) + naturalWidth(t, "[1]", goregular.TTF, size*0.75)
	opts.PageSize.Width = 20 + width + 0.01
	opts.Footnotes = []flow.Footnote{{ID: "note", Text: "N"}}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Body"}, {Footnote: "note"}, {Text: "."}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var body, punctuation creator.Text
	for _, text := range modelTexts(model) {
		if textValue(text) == "Body" {
			body = text
		}
		if textValue(text) == "." {
			punctuation = text
		}
	}
	if body.Size == 0 || punctuation.Size == 0 || math.Abs(textBaseline(body)-textBaseline(punctuation)) > 0.000001 || punctuation.X+punctuation.Width > opts.PageSize.Width-10+0.000001 {
		t.Fatal("上标的实际宽度未用于断行")
	}
}

// 检查实际栅格中的标记更小且高于正文，防止只改字号或边界、未移动实际字形。
func TestFootnoteSuperscriptRendersSmallerAndAboveBody(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Size = 20
	opts.Footnotes = []flow.Footnote{{ID: "note", Text: "NOTE"}}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "HH"}, {Footnote: "note"}, {Text: "HH"}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	if len(texts) < 3 || textValue(texts[0]) != "HH" || textValue(texts[1]) != "[1]" {
		t.Fatal("正文或上标丢失")
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
	ink := func(text creator.Text) (int, int) {
		top, bottom := img.Bounds().Max.Y, -1
		for y := int(math.Floor(text.Y * scale)); y < int(math.Ceil((text.Y+text.Height)*scale)); y++ {
			for x := int(math.Floor(text.X * scale)); x < int(math.Ceil((text.X+text.Width)*scale)); x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if min(r, g, b) < 0x8000 {
					top, bottom = min(top, y), max(bottom, y)
				}
			}
		}
		return top, bottom
	}
	bodyTop, bodyBottom := ink(texts[0])
	markTop, markBottom := ink(texts[1])
	if markBottom < markTop || markTop >= bodyTop || markBottom >= bodyBottom || markBottom-markTop >= bodyBottom-bodyTop || float64(markBottom-markTop) < texts[1].Size*scale*0.55 {
		t.Fatalf("上标画面未正确缩小、上移或发生裁切: body=%d..%d marker=%d..%d", bodyTop, bodyBottom, markTop, markBottom)
	}
}
