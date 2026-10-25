package creator

import "testing"

func TestPruneUnusedFontsKeepsReferencesInAllTypedResources(t *testing.T) {
	names := []string{"body", "template", "clip", "pattern", "annotation", "composite", "figure", "table", "header", "code", "caption", "continuation", "unused"}
	fonts := make([]Font, len(names))
	for i, name := range names {
		fonts[i] = Font{Name: name}
	}
	pattern := &Pattern{Items: []Item{Text{Font: "pattern", Value: "Pattern"}}}
	doc := Document{Fonts: fonts,
		Templates:   []TemplatePage{{Items: []Item{Text{Font: "template", Value: "Template"}}}},
		Annotations: []AnnotationPage{{Items: []Annotation{{Items: []Item{Text{Font: "annotation", Value: "Annotation"}}}}}},
		Composites: []CompositeGraphicUnit{{Items: []Item{
			Text{Font: "composite", Value: "Composite"},
			Figure{Caption: "Figure", CaptionStyle: &CaptionStyle{Font: "figure"}},
		}}},
		Pages: []Page{{Items: []Item{
			PageBlock{Items: []Item{Text{Font: "body", Value: "Body"}}},
			Path{FillColor: &Color{Pattern: pattern}, Clips: &Clips{Items: []Clip{{Areas: []ClipArea{{Text: &ClipText{Font: "clip", Value: "Clip"}}}}}}},
			Table{HeaderRows: 1, Style: TableStyle{HeaderFont: "header", CodeFont: "code"},
				Rows:    []TableRow{{Cells: []TableCell{{Value: "Header"}}}, {Cells: []TableCell{{Font: "table", Runs: []TextRun{{Text: "Code", Code: true}}}}}},
				Caption: "Caption", CaptionStyle: &CaptionStyle{Font: "caption"}, ContinuationCaption: "Continued", ContinuationCaptionStyle: &CaptionStyle{Font: "continuation"}},
		}}},
	}
	doc.PruneUnusedFonts()
	if len(doc.Fonts) != len(names)-1 {
		t.Fatalf("字体引用收集不完整: got=%d want=%d", len(doc.Fonts), len(names)-1)
	}
	for i, font := range doc.Fonts {
		if font.Name != names[i] {
			t.Fatalf("字体被误删或改变顺序: index=%d name=%q want=%q", i, font.Name, names[i])
		}
	}
	if len(fonts) != len(names) || fonts[len(fonts)-1].Name != "unused" {
		t.Fatal("清理修改了调用方的原始切片")
	}
}

func TestPruneUnusedFontsPreservesProviderAndRawResourceInputs(t *testing.T) {
	for _, doc := range []Document{
		{},
		{Pages: []Page{{}}, PublicRes: []PublicResource{{Name: "Public.xml", Data: []byte(`<Res/>`)}}},
		{Pages: []Page{{Resources: []PageResource{{Data: []byte(`<Res/>`)}}}}},
	} {
		doc.Fonts = []Font{{Name: "first"}, {Name: "second"}}
		doc.PruneUnusedFonts()
		if len(doc.Fonts) != 2 || doc.Fonts[0].Name != "first" || doc.Fonts[1].Name != "second" {
			t.Fatal("没有完整页面或存在原始资源时更改了字体注册顺序")
		}
	}
}
