package flow_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 主题切换应真正影响标题、正文行距、图注和表格，而不改变默认主题。
func TestPrintThemesControlAuthoredTypography(t *testing.T) {
	for _, test := range []struct {
		name                   string
		theme                  flow.Theme
		headings               []float64
		bodySize, bodyLine     float64
		regularHeadingLevels   int
		captionRed             uint8
		captionSize, tableSize float64
	}{
		{"fluent2", flow.Fluent2Theme(), []float64{24, 21, 18, 15, 12, 10.5}, 10.5, 15, 0, 0x61, 9, 10.5},
		{"material2", flow.Material2Theme(), []float64{72, 45, 36, 25.5, 18, 15}, 12, 18, 5, 0x75, 9, 10.5},
		{"material3", flow.Material3Theme(), []float64{24, 21, 18, 16.5, 12, 10.5}, 12, 18, 4, 0x49, 9, 10.5},
		{"carbon", flow.CarbonTheme(), []float64{31.5, 24, 21, 15, 12, 10.5}, 12, 18, 4, 0x52, 9, 10.5},
		{"bootstrap5", flow.Bootstrap5Theme(), []float64{30, 24, 21, 18, 15, 12}, 12, 18, 0, 0x59, 10.5, 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkThemeTypography(t, test.theme, test.headings, test.bodySize, test.bodyLine, test.regularHeadingLevels, test.captionRed, test.captionSize, test.tableSize)
		})
	}
}

func checkThemeTypography(t *testing.T, theme flow.Theme, headings []float64, bodySize, bodyLine float64, regularHeadingLevels int, captionRed uint8, captionSize, tableSize float64) {
	t.Helper()
	opts := smallOptions()
	opts.PageSize = creator.A4
	opts.Theme = theme
	d := flow.New(opts)
	for level := 1; level <= 6; level++ {
		d.AddHeading(level, fmt.Sprintf("Level %d", level))
	}
	d.AddParagraph("Body one\nBody two")
	d.AddFixed(flow.Fixed{Width: 20, Height: 5, Caption: "Caption", Items: []creator.Item{fixedRect(0, 0, 20, 5)}})
	d.AddTable(flow.Table{Rows: [][]string{{"Cell"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]float64)
	fonts := make(map[string]string)
	var firstY, secondY float64
	for _, text := range modelTexts(model) {
		value := textValue(text)
		seen[value] = text.Size * 72 / 25.4
		fonts[value] = text.Font
		switch value {
		case "Body one":
			firstY = text.Y
		case "Body two":
			secondY = text.Y
		case "Caption":
			if text.X != opts.MarginLeft || text.FillColor == nil || text.FillColor.R != captionRed {
				t.Fatal("图注没有采用主题的左对齐和次要文字色")
			}
		}
	}
	for i, size := range headings {
		value := fmt.Sprintf("Level %d", i+1)
		font := "Flow-Bold"
		if i < regularHeadingLevels {
			font = "Flow-Regular"
		}
		if math.Abs(seen[value]-size) > 0.000001 || fonts[value] != font {
			t.Fatalf("标题角色字号或字体未应用: %q size=%g font=%q", value, seen[value], fonts[value])
		}
	}
	for value, size := range map[string]float64{"Body one": bodySize, "Body two": bodySize, "Caption": captionSize, "Cell": tableSize} {
		if math.Abs(seen[value]-size) > 0.000001 {
			t.Fatalf("主题字号未应用: %q got=%g want=%g", value, seen[value], size)
		}
	}
	if firstY == 0 || math.Abs(secondY-firstY-bodyLine*25.4/72) > 0.000001 {
		t.Fatal("正文没有采用主题的行高")
	}
	if !reflect.DeepEqual(flow.DefaultOptions().Theme, flow.MaterialTheme()) {
		t.Fatal("新增主题改变了默认 Material 预设")
	}
}

func TestFluent2ThemeInstancesDoNotShareMutableColors(t *testing.T) {
	first, second := flow.Fluent2Theme(), flow.Fluent2Theme()
	first.Body.Color.R = 0
	first.Caption.Color.R = 0
	first.BorderColor.R = 0
	first.HeaderFill.R = 0
	if second.Body.Color.R != 0x24 || second.Caption.Color.R != 0x61 || second.BorderColor.R != 0xd1 || second.HeaderFill.R != 0xf5 {
		t.Fatal("修改一个主题影响了其他主题实例")
	}
}

func TestMaterial3ThemeInstancesDoNotShareMutableColors(t *testing.T) {
	first, second := flow.Material3Theme(), flow.Material3Theme()
	first.Body.Color.R = 0
	first.Caption.Color.R = 0
	first.BorderColor.R = 0
	first.HeaderFill.R = 0
	if second.Body.Color.R != 0x1d || second.Caption.Color.R != 0x49 || second.BorderColor.R != 0xca || second.HeaderFill.R != 0xf3 {
		t.Fatal("修改一个 Material 3 主题影响了其他实例")
	}
}

func TestMaterial2ThemeInstancesDoNotShareMutableColors(t *testing.T) {
	first, second := flow.Material2Theme(), flow.Material2Theme()
	first.Body.Color.R = 0
	first.Caption.Color.R = 0
	first.BorderColor.R = 0
	first.HeaderFill.R = 0
	if second.Body.Color.R != 0x21 || second.Caption.Color.R != 0x75 || second.BorderColor.R != 0xe0 || second.HeaderFill.R != 0xf5 {
		t.Fatal("修改一个 Material 2 主题影响了其他实例")
	}
}

func TestAdditionalThemeInstancesDoNotShareMutableColors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		newTheme func() flow.Theme
		reds     [4]uint8
	}{
		{"carbon", flow.CarbonTheme, [4]uint8{0x16, 0x52, 0xc6, 0xe0}},
		{"bootstrap5", flow.Bootstrap5Theme, [4]uint8{0x21, 0x59, 0xde, 0xf8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := tc.newTheme(), tc.newTheme()
			first.Body.Color.R, first.Caption.Color.R, first.BorderColor.R, first.HeaderFill.R = 0, 0, 0, 0
			if got := [4]uint8{second.Body.Color.R, second.Caption.Color.R, second.BorderColor.R, second.HeaderFill.R}; got != tc.reds {
				t.Fatal("修改主题污染了其他主题实例")
			}
		})
	}
}

// 核对主题实际输出的多行间距与段间距，避免只有字号改变而行高、段距仍沿用默认主题。
func TestCarbonAndBootstrap5UseTheirOwnLineAndParagraphSpacing(t *testing.T) {
	for _, tc := range []struct {
		name                                                string
		theme                                               flow.Theme
		headingLine, captionLine, tableLine, paragraphAfter float64
	}{
		{"carbon", flow.CarbonTheme(), 31.5 * 1.199, 12, 13.5, 8},
		{"bootstrap5", flow.Bootstrap5Theme(), 36, 15.75, 18, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := flow.DefaultOptions()
			o.Theme = tc.theme
			d := flow.New(o)
			d.AddHeading(1, "Head one\nHead two")
			d.AddParagraph("Para one")
			d.AddParagraph("Para two")
			d.AddTable(flow.Table{Rows: [][]string{{"Cell one\nCell two"}}})
			d.AddFixed(flow.Fixed{Width: 20, Height: 5, Caption: "Cap one\nCap two", Items: []creator.Item{fixedRect(0, 0, 20, 5)}})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			texts := map[string]creator.Text{}
			for _, text := range modelTexts(model) {
				texts[textValue(text)] = text
			}
			for _, pair := range []struct {
				first, second string
				gap           float64
			}{
				{"Head one", "Head two", tc.headingLine}, {"Cap one", "Cap two", tc.captionLine},
				{"Cell one", "Cell two", tc.tableLine}, {"Para one", "Para two", 18 + tc.paragraphAfter},
			} {
				if math.Abs(texts[pair.second].Y-texts[pair.first].Y-pair.gap*25.4/72) > 0.000001 {
					t.Fatalf("%s 的实际间距没有采用主题参数", pair.first)
				}
			}
		})
	}
}

func TestBootstrap5HeadingClearsInheritedParagraphSpaceBefore(t *testing.T) {
	var tops []float64
	for _, space := range []float64{0, 6} {
		o := flow.DefaultOptions()
		o.Theme = flow.Bootstrap5Theme()
		o.Theme.Body.SpaceBefore = space
		d := flow.New(o)
		d.AddParagraph("Prefix")
		d.AddHeading(2, "Heading")
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range modelTexts(model) {
			if textValue(text) == "Heading" {
				tops = append(tops, text.Y)
			}
		}
	}
	if len(tops) != 2 || math.Abs(tops[0]-tops[1]) > 0.000001 {
		t.Fatalf("Bootstrap 标题段前零值没有清除正文段前间距: %v", tops)
	}
}
