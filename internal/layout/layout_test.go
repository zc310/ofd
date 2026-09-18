package layout

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func textItems(document *creator.Document) []creator.Text {
	var result []creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				result = append(result, text)
			}
		}
	}
	return result
}

func TestBuildWrapsLongParagraph(t *testing.T) {
	options := DefaultOptions()
	options.PageWidth = 60
	options.PageHeight = 80
	options.MarginLeft = 10
	options.MarginRight = 10
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: strings.Repeat("word ", 40)}},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(textItems(document)) < 2 {
		t.Fatalf("长段落应换行为多个文字对象，实际 %d", len(textItems(document)))
	}
}

func TestBuildPaginatesLongContent(t *testing.T) {
	options := DefaultOptions()
	options.PageWidth = 60
	options.PageHeight = 60
	options.MarginTop = 10
	options.MarginBottom = 10
	options.MarginLeft = 10
	options.MarginRight = 10
	blocks := make([]Block, 0, 20)
	for index := 0; index < 20; index++ {
		blocks = append(blocks, Block{Kind: KindParagraph, Inlines: []Inline{{Text: "段落内容"}}})
	}
	document, err := Build(&Document{Blocks: blocks}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) < 2 {
		t.Fatalf("内容应分页，实际页数 %d", len(document.Pages))
	}
}

func TestBuildNeverEmitsWhitespaceOnlyText(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "a"}, {Text: "   ", Bold: true}, {Text: "b"}},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	for _, text := range textItems(document) {
		if strings.TrimSpace(text.Value) == "" {
			t.Fatalf("出现只含空白的文字对象: %q", text.Value)
		}
	}
}

func TestBuildEmptyDocumentHasOnePage(t *testing.T) {
	document, err := Build(&Document{}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) != 1 {
		t.Fatalf("空文档应有一页，实际 %d", len(document.Pages))
	}
}

func TestBuildOrdersContentTopToBottom(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{
		{Kind: KindHeading, Level: 1, Inlines: []Inline{{Text: "标题"}}},
		{Kind: KindParagraph, Inlines: []Inline{{Text: "第一段"}}},
		{Kind: KindParagraph, Inlines: []Inline{{Text: "第二段"}}},
	}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var ys []float64
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				ys = append(ys, text.Y)
			}
		}
	}
	if len(ys) < 3 {
		t.Fatalf("文字对象不足: %d", len(ys))
	}
	// creator/渲染器的 Y 以页面顶部为原点，越靠前的内容 Y 越小。
	if !(ys[0] < ys[1] && ys[1] < ys[2]) {
		t.Fatalf("内容未按从上到下的顺序排版: %v", ys)
	}
}

func narrowOptions() Options {
	options := DefaultOptions()
	options.PageWidth = 60
	options.PageHeight = 200
	options.MarginTop = 10
	options.MarginBottom = 10
	options.MarginLeft = 10
	options.MarginRight = 10
	return options
}

func lineTexts(t *testing.T, document *creator.Document) []string {
	t.Helper()
	var values []string
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				values = append(values, text.Value)
			}
		}
	}
	return values
}

func TestBuildAvoidsLeadingCJKClosingPunctuation(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "一二三四五六七八九。"}},
	}}}, narrowOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	values := lineTexts(t, document)
	if len(values) < 2 {
		t.Fatalf("应换行为多行，实际 %d", len(values))
	}
	for _, value := range values {
		if strings.HasPrefix(value, "。") {
			t.Fatalf("闭标点出现在行首: %q", value)
		}
	}
}

func TestBuildKeepsLatinWordsIntact(t *testing.T) {
	source := "alpha beta gamma delta epsilon zeta eta theta"
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: source}},
	}}}, narrowOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	values := lineTexts(t, document)
	if len(values) < 2 {
		t.Fatalf("应换行为多行，实际 %d", len(values))
	}
	reconstructed := strings.Join(strings.Fields(strings.Join(values, " ")), " ")
	if reconstructed != source {
		t.Fatalf("换行破坏了单词: %q != %q", reconstructed, source)
	}
}

func colorEqual(color *creator.Color, r, g, b uint8) bool {
	return color != nil && color.R == r && color.G == g && color.B == b
}

func TestBuildCodeTextAlignedWithBackground(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindCode,
		Code: "package main\n\nfunc main() {}",
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var bands []creator.Path
	var texts []creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Path:
				if colorEqual(value.FillColor, 244, 245, 247) {
					bands = append(bands, value)
				}
			case creator.Text:
				if colorEqual(value.FillColor, 166, 30, 78) {
					texts = append(texts, value)
				}
			}
		}
	}
	if len(bands) == 0 || len(texts) == 0 {
		t.Fatalf("代码背景或文字缺失: bands=%d texts=%d", len(bands), len(texts))
	}
	for index := range texts {
		baseline := texts[index].Y + texts[index].Height
		aligned := false
		for _, band := range bands {
			if baseline >= band.Y && baseline <= band.Y+band.Height {
				aligned = true
				break
			}
		}
		if !aligned {
			t.Fatalf("第 %d 行代码基线 %.3f 未落在任何代码背景内", index, baseline)
		}
	}
}

func TestBuildTableTextAlignedWithHeader(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindTable,
		Table: &Table{
			Header: []Cell{{{Text: "名称"}}, {{Text: "数量"}}},
			Rows:   [][]Cell{{{{Text: "苹果"}}, {{Text: "3"}}}},
		},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var header *creator.Path
	var texts []creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Path:
				if colorEqual(value.FillColor, 239, 242, 245) {
					copied := value
					header = &copied
				}
			case creator.Text:
				texts = append(texts, value)
			}
		}
	}
	if header == nil {
		t.Fatalf("未找到表头背景")
	}
	found := false
	for _, text := range texts {
		baseline := text.Y + text.Height
		if baseline >= header.Y && baseline <= header.Y+header.Height {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("表头文字未落在表头背景 [%.3f, %.3f] 内", header.Y, header.Y+header.Height)
	}
}

func TestBuildCodeLineSpacingUsesCodeLineHeight(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindCode,
		Code: "a\nb\nc",
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var tops []float64
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if path, ok := item.(creator.Path); ok && colorEqual(path.FillColor, 244, 245, 247) {
				tops = append(tops, path.Y)
			}
		}
	}
	if len(tops) < 2 {
		t.Fatalf("代码背景不足: %d", len(tops))
	}
	want := ptToMM(options.MonoSize) * options.CodeLineHeight
	for index := 1; index < len(tops); index++ {
		if delta := tops[index] - tops[index-1]; delta < want-0.001 || delta > want+0.001 {
			t.Fatalf("代码行距 %.4f 不符合预期 %.4f", delta, want)
		}
	}
	body := ptToMM(options.BodySize) * options.LineHeight
	if want >= body {
		t.Fatalf("代码行距应小于正文行距: code=%.4f body=%.4f", want, body)
	}
}
