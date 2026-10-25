package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	manifestexport "github.com/zc310/ofd/internal/manifest/export"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func naturalWidth(t *testing.T, text string, data []byte, size float64) float64 {
	t.Helper()
	width := 0.0
	for _, r := range text {
		width += fontAdvance(t, data, r) * size
	}
	return width
}

// 文字恰好放满一行时，纯文本、跨字体尾空白和制表符均不能增加行数或页数。
func TestTrailingWhitespaceDoesNotChangeWrappingAlignmentOrPagination(t *testing.T) {
	for _, align := range []string{"left", "center", "right", "justify"} {
		t.Run(align, func(t *testing.T) {
			opts := smallOptions()
			opts.Fonts = append(opts.Fonts, creator.Font{Name: "Mono", Data: gomono.TTF, FixedWidth: true})
			opts.Theme.Body.Align = align
			opts.Theme.Body.FirstLineIndent = 6
			opts.Theme.Body.SpaceAfter = 0
			opts.PageSize.Width = 20 + 6 + naturalWidth(t, "alpha beta", goregular.TTF, 12*25.4/72) + 0.01
			opts.PageSize.Height = 50
			build := func(t *testing.T, runs []flow.Run) *creator.Document {
				t.Helper()
				d := flow.New(opts)
				for range 8 {
					d.Blocks = append(d.Blocks, flow.Block{Runs: runs})
				}
				model, err := d.Build(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				return model
			}
			want := build(t, []flow.Run{{Text: "alpha beta"}})
			if len(want.Pages) < 2 || len(modelTexts(want)) != 8 {
				t.Fatal("未覆盖单行段落跨页")
			}
			for _, test := range []struct {
				name string
				runs []flow.Run
			}{
				{"one space", []flow.Run{{Text: "alpha beta "}}},
				{"many spaces", []flow.Run{{Text: "alpha beta" + strings.Repeat(" ", 100)}}},
				{"tab", []flow.Run{{Text: "alpha beta\t"}}},
				{"manual break", []flow.Run{{Text: "alpha beta \r\n"}}},
				{"split fonts", []flow.Run{{Text: "alpha beta "}, {Text: " \t ", Font: "Mono"}}},
			} {
				t.Run(test.name, func(t *testing.T) {
					got := build(t, test.runs)
					if !reflect.DeepEqual(got.Pages, want.Pages) {
						t.Fatalf("行尾空白改变排版: pages=%d want=%d lines=%d want=8", len(got.Pages), len(want.Pages), len(modelTexts(got)))
					}
				})
			}
		})
	}
}

// 只有处于行尾的空白忽略宽度，两个单词之间的连续空格仍参与断行和字符定位。
func TestInteriorWhitespaceRetainsItsWidthAndCanCauseWrapping(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	size := opts.Theme.Body.Size * 25.4 / 72
	opts.PageSize.Width = 20 + naturalWidth(t, "alpha beta", goregular.TTF, size) + 0.01
	d := flow.New(opts)
	d.AddParagraph("alpha  beta")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	if len(texts) != 2 || textValue(texts[0]) != "alpha" || textValue(texts[1]) != "beta" {
		t.Fatalf("词间空白未参与断行: %+v", texts)
	}
	d.Options = smallOptions()
	d.Blocks[0].Text = "alpha  beta "
	model, err = d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts = modelTexts(model)
	if len(texts) != 1 || textValue(texts[0]) != "alpha  beta" {
		t.Fatal("词间连续空格丢失")
	}
	if math.Abs(texts[0].Width-naturalWidth(t, "alpha  beta", goregular.TTF, size)) > 0.000001 {
		t.Fatal("词间连续空格宽度错误")
	}
}

// 空格并入相邻文字后，仍使用空格原字体的推进量，兼容普通与等宽字体的双向混排。
func TestMixedFontWhitespaceKeepsOriginalAdvancesAfterMerging(t *testing.T) {
	for _, align := range []string{"left", "center", "right", "justify"} {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reversed=%v", align, reversed), func(t *testing.T) {
				opts := smallOptions()
				opts.Theme.Body.Align = align
				opts.Fonts = append(opts.Fonts, creator.Font{Name: "Mono", Data: gomono.TTF, FixedWidth: true})
				wordFont, spaceFont := "Flow-Regular", "Mono"
				wordData, spaceData := goregular.TTF, gomono.TTF
				if reversed {
					wordFont, spaceFont = spaceFont, wordFont
					wordData, spaceData = spaceData, wordData
				}
				d := flow.New(opts)
				d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "ONE", Font: wordFont}, {Text: "   ", Font: spaceFont}, {Text: "TWO", Font: wordFont}}}}
				model, err := d.Build(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				texts := modelTexts(model)
				if len(texts) != 1 || textValue(texts[0]) != "ONE   TWO" || texts[0].Font != wordFont {
					t.Fatal("空格或相邻文字未保留")
				}
				text := texts[0]
				runes := []rune(textValue(text))
				actual := fontAdvance(t, wordData, runes[len(runes)-1]) * text.Size
				for i, delta := range text.TextCodes[0].DeltaX {
					data := wordData
					if runes[i] == ' ' {
						data = spaceData
					}
					want := fontAdvance(t, data, runes[i]) * text.Size
					if math.Abs(delta-want) > 0.000001 {
						t.Fatalf("字符 %d 的推进量换成了相邻字体: delta=%g want=%g", i, delta, want)
					}
					actual += delta
				}
				if math.Abs(actual-text.Width) > 0.000001 {
					t.Fatalf("实际字符位置与测量不一致: actual=%g boundary=%g", actual, text.Width)
				}
			})
		}
	}
}

// 链接拆成多个对象时，词间原宽与对齐间距都应保留，最终可见字符抵达栏右边界。
func TestMixedFontWhitespaceJustifiesAcrossLinkBoundaries(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Mono", Data: gomono.TTF, FixedWidth: true})
	d := flow.New(opts)
	var runs []flow.Run
	for i := range 20 {
		if i > 0 {
			runs = append(runs, flow.Run{Text: "  ", Font: "Mono"})
		}
		runs = append(runs, flow.Run{Text: "HH", Href: fmt.Sprintf("https://example.com/%d", i)})
	}
	d.Blocks = []flow.Block{{Runs: runs}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	texts := modelTexts(model)
	wrapped := 0
	for start := 0; start < len(texts); {
		end := start + 1
		for end < len(texts) && texts[end].Y == texts[start].Y {
			end++
		}
		for i := start; i < end; i++ {
			text := texts[i]
			if len(text.Actions) != 1 || text.Actions[0].URI == nil || text.Actions[0].URI.URI != fmt.Sprintf("https://example.com/%d", i) {
				t.Fatal("混合字体空格改变链接归属")
			}
			if math.Abs(text.Width-naturalWidth(t, "HH", goregular.TTF, text.Size)-naturalWidth(t, strings.TrimPrefix(textValue(text), "HH"), gomono.TTF, text.Size)) > 0.000001 {
				t.Fatal("链接边界的空格宽度未保留")
			}
			if strings.HasSuffix(textValue(text), "  ") {
				deltas := text.TextCodes[0].DeltaX
				if math.Abs(deltas[len(deltas)-1]-fontAdvance(t, gomono.TTF, ' ')*text.Size) > 0.000001 {
					t.Fatal("连续空格中的字符推进量换成了相邻字体")
				}
			}
		}
		last := texts[end-1]
		if textValue(last) != "HH" {
			t.Fatal("自动行的行尾空白未清除")
		}
		if end < len(texts) {
			if math.Abs(last.X+last.Width-90) > 0.000001 {
				t.Fatal("跨链接伸展后未抵达栏右边界")
			}
			wrapped++
		}
		start = end
	}
	if wrapped < 2 {
		t.Fatal("未覆盖跨链接自动换行")
	}
}

// 相同内容合并成一个对象或通过链接分成多个对象，应渲染到相同的行尾位置。
// 同时保护 OFD 写出与 JSON/YAML 导出往返中的原字体空格推进量。
func TestMixedFontWhitespaceRendersAndSurvivesManifestRoundTrip(t *testing.T) {
	opts := smallOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Mono", Data: gomono.TTF, FixedWidth: true})
	d := flow.New(opts)
	d.Blocks = []flow.Block{
		{Runs: []flow.Run{{Text: "HH"}, {Text: " ", Font: "Mono"}, {Text: "HH"}}},
		{ID: "anchor", Runs: []flow.Run{{Text: "HH"}, {Text: " ", Font: "Mono"}, {Text: "HH", Target: "anchor"}}},
	}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := modelTexts(model)
	var output, repeated, raster bytes.Buffer
	options := creator.CreateOptions{Deterministic: true}
	if err := d.Write(context.Background(), &output, options); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), &repeated, options); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), repeated.Bytes()) {
		t.Fatal("混合字体空格的写出不确定")
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, data []byte) {
		t.Helper()
		report := v.ValidateReader(context.Background(), bytes.NewReader(data), "whitespace.ofd")
		if report.HasErrors() || report.HasWarnings() {
			t.Fatalf("混合字体空格严格校验失败: %+v", report.Issues)
		}
	}
	check(t, output.Bytes())
	if err := converter.Convert(context.Background(), "ofd", "png", output.Bytes(), &raster, converter.Page(1), converter.DPI(144), converter.BgColor(color.White)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raster.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	scale := 144.0 / 25.4
	var edges []int
	for _, text := range []creator.Text{want[0], want[1]} {
		right := -1
		for y := int(text.Y * scale); y < int((text.Y+text.Height)*scale); y++ {
			for x := int(text.X * scale); x < int(90*scale); x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if min(r, g, b) < 0x8000 {
					right = max(right, x)
				}
			}
		}
		edges = append(edges, right)
	}
	if edges[0] < 0 || edges[1] < 0 || math.Abs(float64(edges[0]-edges[1])) > 2 {
		t.Fatalf("合并文字改变空格的实际渲染宽度: merged=%d split=%d", edges[0], edges[1])
	}
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			var exported bytes.Buffer
			if err := manifestexport.WriteManifest(output.Bytes(), &exported, manifestexport.Options{Format: format, AssetRoot: filepath.Join(root, "assets"), AssetPrefix: "assets"}); err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(root, "document."+format)
			if err := os.WriteFile(input, exported.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			loaded, base, err := manifest.Load(input, format)
			if err != nil {
				t.Fatal(err)
			}
			fixed, err := loaded.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			got := modelTexts(&fixed)
			for _, page := range fixed.Pages {
				for _, layer := range page.Layers {
					for _, item := range layer.Items {
						if text, ok := item.(creator.Text); ok {
							got = append(got, text)
						}
					}
				}
			}
			if len(got) != len(want) {
				t.Fatalf("导出丢失文字: got=%d want=%d", len(got), len(want))
			}
			for i, text := range got {
				// XML 写出按四位小数固化长度；导出后的 Value 与 TextCodes 可同时保留同一文字。
				if len(text.TextCodes) != 1 || text.TextCodes[0].Value != textValue(want[i]) || math.Abs(text.X-want[i].X) > 0.0001 || math.Abs(text.Width-want[i].Width) > 0.0001 || len(text.TextCodes[0].DeltaX) != len(want[i].TextCodes[0].DeltaX) || len(text.Actions) != len(want[i].Actions) {
					t.Fatalf("导出改变文字位置或边界: %+v", text)
				}
				for j, delta := range text.TextCodes[0].DeltaX {
					if math.Abs(delta-want[i].TextCodes[0].DeltaX[j]) > 0.0001 {
						t.Fatal("导出丢失原字体空格的推进量")
					}
				}
			}
			data, err := creator.MarshalWithOptions(fixed, options)
			if err != nil {
				t.Fatal(err)
			}
			check(t, data)
		})
	}
}
