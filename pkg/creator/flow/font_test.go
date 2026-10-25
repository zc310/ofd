package flow_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 合法 TTC 由真实 Go 字体组装，不依赖系统字体或不可解析的占位数据。
func fontCollection(fonts ...[]byte) []byte {
	data := make([]byte, 12+len(fonts)*4)
	copy(data, "ttcf")
	binary.BigEndian.PutUint32(data[4:], 0x00010000)
	binary.BigEndian.PutUint32(data[8:], uint32(len(fonts)))
	for i, source := range fonts {
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
		offset := len(data)
		binary.BigEndian.PutUint32(data[12+i*4:], uint32(offset))
		face := append([]byte(nil), source...)
		count := int(binary.BigEndian.Uint16(face[4:]))
		for table := 0; table < count; table++ {
			start := 12 + table*16 + 8
			binary.BigEndian.PutUint32(face[start:], binary.BigEndian.Uint32(face[start:])+uint32(offset))
		}
		data = append(data, face...)
	}
	return data
}

func fontAdvance(t *testing.T, data []byte, r rune) float64 {
	t.Helper()
	face, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	glyph, err := face.GlyphIndex(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	advance, err := face.GlyphAdvance(&buf, glyph, 1000*64, font.HintingNone)
	if err != nil {
		t.Fatal(err)
	}
	return float64(advance) / 64000
}

func latinSubset(t *testing.T, data []byte) []byte {
	t.Helper()
	face, err := fontfile.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	glyphs := []uint16{0}
	for r := rune(32); r < 127; r++ {
		glyphs = append(glyphs, face.GlyphIndex(r))
	}
	subset, err := face.Subset(glyphs, fontfile.SubsetOptions{Tables: fontfile.KeepMinTables})
	if err != nil {
		t.Fatal(err)
	}
	return subset.Write()
}

func TestTTCFaceSelectionMeasuresAndEmbedsSameStandaloneFace(t *testing.T) {
	collection := fontCollection(goregular.TTF, gomono.TTF)
	input := append([]byte(nil), collection...)
	source := &countedFontSource{data: collection}
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Selected", Source: source, FixedWidth: true})
	opts.FontFaces = map[string]int{"Selected": 1}
	opts.Theme.Body.Font = "Selected"
	d := flow.New(opts)
	d.AddTOC()
	d.Blocks = append(d.Blocks, flow.Block{ID: "heading", Kind: "heading", Level: 1, Text: "Selected face"})
	d.AddParagraph("WiWi")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source.opens != 1 {
		t.Fatalf("目录多轮排版重复读取字体: %d", source.opens)
	}
	if !bytes.Equal(collection, input) || opts.FontFaces["Selected"] != 1 {
		t.Fatal("字体选择修改了输入数据")
	}
	var selected creator.Font
	for _, resource := range model.Fonts {
		if resource.Name == "Selected" {
			selected = resource
		}
	}
	if len(selected.Data) == 0 || bytes.HasPrefix(selected.Data, []byte("ttcf")) || selected.Source != nil || selected.Format != "ttf" {
		t.Fatal("没有输出独立的所选字体面")
	}
	if selected.FontName != fullFontName(t, gomono.TTF) || selected.FamilyName != "Go Mono" {
		t.Fatalf("没有读取所选字体面的名称: font_name=%q family=%q", selected.FontName, selected.FamilyName)
	}
	want := fontAdvance(t, gomono.TTF, 'W')
	if math.Abs(fontAdvance(t, selected.Data, 'W')-want) > 0.000001 {
		t.Fatal("实际嵌入的字体不是集合中的第 1 面")
	}
	found := false
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && textValue(text) == "WiWi" {
				found = true
				if text.Font != "Selected" || math.Abs(text.Width-4*want*text.Size) > 0.000001 {
					t.Fatalf("度量与嵌入字体不一致: %+v", text)
				}
				for _, advance := range text.TextCodes[0].DeltaX {
					if math.Abs(advance-want*text.Size) > 0.000001 {
						t.Fatal("字符位置使用了其他字体的度量")
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("未找到所选字体的文字")
	}
	data, err := creator.MarshalWithOptions(*model, creator.CreateOptions{Deterministic: true, PreserveEmbeddedFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if strings.HasSuffix(entry.Name, ".ttc") {
			t.Fatal("OFD 中仍嵌入了整份字体集合")
		}
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(data), "selected.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("所选字体面严格校验失败: %+v", report.Issues)
	}
}

func TestIndependentFontFamiliesMatchStylesAndExplicitRunFonts(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts,
		creator.Font{Name: "Other", FamilyName: "Other family", Data: goregular.TTF},
		creator.Font{Name: "OtherBold", FamilyName: "Other family", Data: gobold.TTF},
		creator.Font{Name: "OtherItalic", FamilyName: "Other family", Data: goitalic.TTF},
		creator.Font{Name: "OtherBoth", FamilyName: "Other family", Data: gobolditalic.TTF})
	opts.FontFamilies = []flow.FontFamily{{Regular: "Other", Bold: "OtherBold", Italic: "OtherItalic", BoldItalic: "OtherBoth"}}
	d := flow.New(opts)
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{
		{Text: "DefaultBold ", Bold: true},
		{Text: "OtherBold ", Font: "Other", Bold: true},
		{Text: "OtherItalic ", Font: "Other", Italic: true},
		{Text: "OtherBoth ", Font: "OtherBold", Italic: true},
	}})
	d.AddParagraph("No style", flow.TextStyle{Font: "Other"})
	d.Blocks = append(d.Blocks, flow.Block{Kind: "table", Table: &flow.Table{Grid: [][]flow.TableCell{{{Runs: []flow.Run{{Text: "CellBold", Bold: true}}, Style: &flow.TextStyle{Font: "Other"}}}}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{"DefaultBold": "Flow-Bold", "OtherBold": "OtherBold", "OtherItalic": "OtherItalic", "OtherBoth": "OtherBoth", "No style": "Other", "CellBold": "OtherBold"}
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				value := strings.TrimSpace(textValue(text))
				if want, ok := wants[value]; ok {
					if text.Font != want {
						t.Fatalf("%q 使用 %q，期望 %q", value, text.Font, want)
					}
					delete(wants, value)
				}
			}
		}
	}
	if len(wants) != 0 {
		t.Fatalf("字体片段丢失: %v", wants)
	}
	for _, resource := range model.Fonts {
		if resource.Name == "OtherBold" && !resource.Bold {
			t.Fatal("粗体资源没有正确标注")
		}
		if resource.Name == "OtherBoth" && (!resource.Bold || !resource.Italic) {
			t.Fatal("粗斜体资源标注丢失")
		}
	}
}

func TestCustomBodyDoesNotUseDefaultLatinStyleFonts(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Custom", Data: goregular.TTF})
	opts.Theme.Body.Font = "Custom"
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Keep custom", Bold: true}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := model.Pages[0].Items[0].(creator.Text)
	if text.Font != "Custom" {
		t.Fatalf("自定义正文被默认拉丁粗体替换: %q", text.Font)
	}
}

func TestFallbackSelectsMatchingFamilyStyle(t *testing.T) {
	opts := flow.DefaultOptions()
	// 主字体子集不含 Omega，回退必须选择已注册的粗体字体面。
	primary := latinSubset(t, goregular.TTF)
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Primary", Data: primary}, creator.Font{Name: "Fallback", Data: goregular.TTF}, creator.Font{Name: "FallbackBold", Data: gobold.TTF})
	opts.Theme.Body.Font = "Primary"
	opts.FallbackFonts = []string{"Fallback"}
	opts.FontFamilies = []flow.FontFamily{{Regular: "Fallback", Bold: "FallbackBold"}}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Omega Ω", Bold: true}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && strings.Contains(textValue(text), "Ω") {
			found = true
			if text.Font != "FallbackBold" {
				t.Fatalf("回退字体丢失粗体: %q", text.Font)
			}
		}
	}
	if !found {
		t.Fatal("缺字回退丢失文字")
	}
}

func TestMonospacedFallbackPreservesFixedWidthCategory(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "MonoPrimary", Data: latinSubset(t, gomono.TTF), FixedWidth: true}, creator.Font{Name: "Proportional", Data: goregular.TTF}, creator.Font{Name: "MonoFallback", Data: gomono.TTF, FixedWidth: true})
	opts.FallbackFonts = []string{"Proportional", "MonoFallback"}
	d := flow.New(opts)
	d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Code Ω", Font: "MonoPrimary"}}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && strings.Contains(textValue(text), "Ω") {
			found = true
			if text.Font != "MonoFallback" {
				t.Fatalf("等宽文字回退到了比例字体: %q", text.Font)
			}
		}
	}
	if !found {
		t.Fatal("等宽缺字回退丢失文字")
	}
}

func TestFontConfigurationRejectsInvalidSelections(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*flow.Options)
		want   string
	}{
		{"negative index", func(o *flow.Options) { o.FontFaces = map[string]int{"Flow-Regular": -1} }, "负数"},
		{"single face index", func(o *flow.Options) { o.FontFaces = map[string]int{"Flow-Regular": 1} }, "只支持索引 0"},
		{"unknown selection", func(o *flow.Options) { o.FontFaces = map[string]int{"unknown": 0} }, "未注册"},
		{"collection overflow", func(o *flow.Options) {
			o.Fonts[0].Data = fontCollection(goregular.TTF, gomono.TTF)
			o.FontFaces = map[string]int{"Flow-Regular": 2}
		}, "超出集合范围"},
		{"missing regular", func(o *flow.Options) { o.FontFamilies = []flow.FontFamily{{Bold: "Flow-Bold"}} }, "regular"},
		{"unknown style", func(o *flow.Options) { o.FontFamilies = []flow.FontFamily{{Regular: "Flow-Regular", Bold: "unknown"}} }, "未注册"},
		{"duplicate role", func(o *flow.Options) {
			o.FontFamilies = []flow.FontFamily{{Regular: "Flow-Regular", Bold: "Flow-Regular"}}
		}, "多个样式"},
		{"duplicate family", func(o *flow.Options) {
			o.FontFamilies = []flow.FontFamily{{Regular: "Flow-Regular"}, {Regular: "Flow-Regular"}}
		}, "多个字体族"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := flow.DefaultOptions()
			test.change(&opts)
			d := flow.New(opts)
			d.AddParagraph("Text")
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误 = %v，期望包含 %q", err, test.want)
			}
		})
	}
}

// 仅能提取文字不足以证明显示正确；保护真实斜体被反向合成后压成细线的问题。
func TestEmbeddedItalicStylesRenderAtReadableHeight(t *testing.T) {
	for _, bold := range []bool{false, true} {
		t.Run(map[bool]string{false: "italic", true: "bold italic"}[bold], func(t *testing.T) {
			opts := smallOptions()
			opts.Theme.Body.Size = 24
			d := flow.New(opts)
			d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: "Italic", Italic: true, Bold: bold}}}}
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
			minY, maxY := img.Bounds().Max.Y, -1
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					r, g, b, a := img.At(x, y).RGBA()
					if a > 0 && r < 0xcccc && g < 0xcccc && b < 0xcccc {
						minY = min(minY, y)
						maxY = max(maxY, y)
					}
				}
			}
			if maxY-minY < 20 {
				t.Fatalf("24pt 斜体字形的可见高度过小: %dpx", maxY-minY)
			}
		})
	}
}
