package flow_test

import (
	"context"
	"strings"
	"testing"

	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

func fullFontName(t *testing.T, data []byte) string {
	t.Helper()
	face, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	name, err := face.Name(&buffer, sfnt.NameIDFull)
	if err != nil || name == "" {
		t.Fatalf("测试字体缺少实际名称: name=%q err=%v", name, err)
	}
	return name
}

func TestFlowFontNamesUseEmbeddedFacesAndKeepResourceAliases(t *testing.T) {
	d := flow.New()
	d.Blocks = []flow.Block{{Runs: []flow.Run{
		{Text: "Regular "}, {Text: "Bold ", Bold: true},
		{Text: "Italic ", Italic: true}, {Text: "Both", Bold: true, Italic: true},
	}}}
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]creator.Font{}
	for i, font := range model.Fonts {
		name := fullFontName(t, d.Options.Fonts[i].Data)
		if font.Name != d.Options.Fonts[i].Name || font.FontName != name || font.FamilyName != "Go" {
			t.Fatalf("字体名称或资源别名错误: alias=%q font_name=%q family=%q want=%q", font.Name, font.FontName, font.FamilyName, name)
		}
		want[name] = font
	}
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && !strings.HasPrefix(text.Font, "Flow-") {
			t.Fatalf("文字模型的内部引用被改写: %+v", text)
		}
	}
	for _, preserve := range []bool{false, true} {
		data, err := creator.MarshalWithOptions(*model, creator.CreateOptions{Deterministic: true, PreserveEmbeddedFonts: preserve})
		if err != nil {
			t.Fatal(err)
		}
		ofd, err := parser.NewOFD(data)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ofd.Close() })
		for _, font := range ofd.Documents[0].Fonts() {
			expected, ok := want[font.FontName]
			if !ok || font.FamilyName != expected.FamilyName || font.Bold != expected.Bold || font.Italic != expected.Italic {
				t.Fatalf("输出未使用实际字体名称及样式: %+v", font)
			}
		}
	}
	for _, font := range d.Options.Fonts {
		if font.FontName != "" {
			t.Fatal("排版修改了原始字体配置")
		}
	}
}

func TestFlowFontNamesPreserveExplicitMetadata(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Alias", FontName: "Explicit Face", FamilyName: "Explicit Family", Data: goregular.TTF})
	d := flow.New(opts)
	d.AddParagraph("Text", flow.TextStyle{Font: "Alias"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	font := model.Fonts[len(model.Fonts)-1]
	if font.Name != "Alias" || font.FontName != "Explicit Face" || font.FamilyName != "Explicit Family" {
		t.Fatalf("显式字体元数据被覆盖: %+v", font)
	}
}

func TestFlowFontWithoutNameTableFallsBackToAlias(t *testing.T) {
	font, err := fontfile.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	delete(font.Tables, "name")
	source := &countedFontSource{data: font.Write()}
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "Unnamed", Source: source})
	d := flow.New(opts)
	d.AddTOC()
	d.Blocks = append(d.Blocks, flow.Block{ID: "target", Kind: "heading", Level: 1, Text: "Title"})
	d.AddParagraph("Text", flow.TextStyle{Font: "Unnamed"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Fonts[len(model.Fonts)-1]; got.FontName != "" || got.FamilyName != "" {
		t.Fatalf("缺少名称时使用了修复器补造的名称: font_name=%q family=%q", got.FontName, got.FamilyName)
	}
	if source.opens != 1 {
		t.Fatalf("名称解析重复读取字体来源: %d", source.opens)
	}
	data, err := creator.Marshal(*model)
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	found := false
	for _, got := range ofd.Documents[0].Fonts() {
		found = found || got.FontName == "Unnamed"
	}
	if !found {
		t.Fatal("没有回退到资源别名")
	}
}
