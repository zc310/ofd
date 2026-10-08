package layout

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
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

func TestInlineCodeKeepsExtensionOnSameLine(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindParagraph,
		Inlines: []Inline{
			{Text: "把扩展名改成"},
			{Text: ".zip", Code: true},
			{Text: "，用解压工具看里面。"},
		},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	// Text.Font 是逻辑字体资源名（MD-N），等宽族名记录在字体资源的 FamilyName 上。
	families := make(map[string]string, len(document.Fonts))
	for _, font := range document.Fonts {
		families[font.Name] = font.FamilyName
	}
	var beforeY, codeY float64
	var foundBefore, foundCode bool
	for _, page := range document.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			if strings.Contains(text.Value, "扩展名改成") {
				beforeY = text.Y
				foundBefore = true
			}
			if strings.Contains(text.Value, ".zip") {
				codeY = text.Y
				foundCode = true
				if family := families[text.Font]; family != "Consolas" {
					t.Fatalf("行内代码字体 = %q（资源名 %q）", family, text.Font)
				}
			}
		}
	}
	if !foundBefore || !foundCode {
		t.Fatalf("缺少前文或扩展名: before=%v code=%v", foundBefore, foundCode)
	}
	if beforeY != codeY {
		t.Fatalf("扩展名被拆到另一行: beforeY=%v codeY=%v", beforeY, codeY)
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
	// 代码背景已合并成整块面板，行距要按文字基线判断，不能再按背景条带。
	var baselines []float64
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && colorEqual(text.FillColor, 166, 30, 78) {
				baselines = append(baselines, text.Y+text.Height)
			}
		}
	}
	if len(baselines) != 3 {
		t.Fatalf("代码行数不符预期: %d", len(baselines))
	}
	want := ptToMM(options.MonoSize) * options.CodeLineHeight
	for index := 1; index < len(baselines); index++ {
		if delta := baselines[index] - baselines[index-1]; delta < want-0.001 || delta > want+0.001 {
			t.Fatalf("代码行距 %.4f 不符合预期 %.4f", delta, want)
		}
	}
	body := ptToMM(options.BodySize) * options.LineHeight
	if want >= body {
		t.Fatalf("代码行距应小于正文行距: code=%.4f body=%.4f", want, body)
	}
}

// 代码背景已从逐行色带改为整块圆角面板，这里守住面板边界、内边距和跨页行为。
func TestBuildCodeRendersSingleRoundedPanelPerPage(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindCode,
		Code: "package main\n\nfunc main() {\n\tprintln(1)\n}",
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) != 1 {
		t.Fatalf("页数不符预期: %d", len(document.Pages))
	}
	panels := codePanels(document)
	if len(panels) != 1 {
		t.Fatalf("同一页的代码背景应合并成一块，实际 %d 块", len(panels))
	}
	panel := panels[0]
	if !strings.Contains(panel.Data, " A ") {
		t.Fatalf("代码背景应使用圆角路径，实际 %q", panel.Data)
	}
	if options.CodeRadius <= 0 {
		t.Skip("未配置圆角半径")
	}
	if _, err := models.ParsePathData(panel.Data); err != nil {
		t.Fatalf("圆角路径语法非法: %v (%s)", err, panel.Data)
	}
	// 面板高度 = 代码行总高 + 上下内边距，宽度与版心一致。
	want := 5*ptToMM(options.MonoSize)*options.CodeLineHeight + options.CodePaddingY*2
	if math.Abs(panel.Height-want) > 0.01 {
		t.Fatalf("面板高度 %.3f 不符预期 %.3f", panel.Height, want)
	}
	if math.Abs(panel.Width-(options.PageWidth-options.MarginLeft-options.MarginRight)) > 0.01 {
		t.Fatalf("面板宽度 %.3f 与版心宽不符", panel.Width)
	}
	// 代码文字必须落在面板内缩范围内。
	for _, item := range document.Pages[0].Items {
		text, ok := item.(creator.Text)
		if !ok || !colorEqual(text.FillColor, 166, 30, 78) {
			continue
		}
		if text.X < options.MarginLeft+options.CodePaddingX-0.01 {
			t.Fatalf("代码文字左边距 %.3f 未留出内边距", text.X)
		}
	}
}

// 代码块跨页时每页各起一块面板，且面板不得越出版心上下边界。
func TestBuildCodePanelStaysInsideContentAreaAcrossPages(t *testing.T) {
	options := DefaultOptions()
	var code strings.Builder
	for i := 0; i < 120; i++ {
		code.WriteString("line\n")
	}
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindCode,
		Code: code.String(),
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) < 2 {
		t.Fatalf("代码块应跨页，实际 %d 页", len(document.Pages))
	}
	for index, page := range document.Pages {
		panels := codePanelsOf(page)
		if len(panels) != 1 {
			t.Fatalf("第 %d 页代码背景应恰好一块，实际 %d 块", index+1, len(panels))
		}
		panel := panels[0]
		if panel.Y < options.MarginTop-0.01 {
			t.Fatalf("第 %d 页面板顶端 %.3f 越出版心", index+1, panel.Y)
		}
		if panel.Y+panel.Height > options.PageHeight-options.MarginBottom+0.01 {
			t.Fatalf("第 %d 页面板底端 %.3f 越出版心", index+1, panel.Y+panel.Height)
		}
	}
}

// 围栏代码块的语言标记应绘制在面板顶部，字号小于代码正文。
func TestBuildCodeLanguageLabelRendersInsidePanel(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{Blocks: []Block{{
		Kind:     KindCode,
		Code:     "package main",
		CodeLang: "go",
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	panels := codePanels(document)
	if len(panels) != 1 {
		t.Fatalf("代码背景应恰好一块，实际 %d 块", len(panels))
	}
	var label *creator.Text
	for index, item := range document.Pages[0].Items {
		text, ok := item.(creator.Text)
		if ok && text.Value == "go" {
			copied := text
			label = &copied
			// 底色必须排在语言标签之前，否则标签会被面板盖住。
			_ = index
		}
	}
	if label == nil {
		t.Fatal("未绘制语言标签")
	}
	if label.Size >= ptToMM(options.MonoSize) {
		t.Fatalf("语言标签字号 %.3f 应小于代码字号 %.3f", label.Size, ptToMM(options.MonoSize))
	}
	if !colorEqual(label.FillColor, 0x6b, 0x74, 0x80) {
		t.Fatalf("语言标签颜色 %v 不符预期", label.FillColor)
	}
	// 标签与代码都在面板内。
	for _, item := range document.Pages[0].Items {
		text, ok := item.(creator.Text)
		if !ok || (text.Value != "go" && text.Value != "package main") {
			continue
		}
		if text.Y < panels[0].Y || text.Y+text.Height > panels[0].Y+panels[0].Height {
			t.Fatalf("文字 %q 未落在代码面板内", text.Value)
		}
	}
}

// 没有语言标记的代码块不应多出标签文字，面板也不应因此变高。
func TestBuildCodeWithoutLanguageLabelHasNoLabel(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindCode,
		Code: "package main",
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	for _, item := range document.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && text.Value != "package main" {
			t.Fatalf("出现了预期外的文字 %q", text.Value)
		}
	}
}

// 行内代码应有底色矩形，且底色在图元顺序上先于文字。
func TestBuildInlineCodeDrawsBackgroundBeforeText(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "调用 "}, {Text: "Run()", Code: true}, {Text: " 完成"}},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	backgroundIndex, textIndex := -1, -1
	for index, item := range document.Pages[0].Items {
		switch value := item.(type) {
		case creator.Path:
			if colorEqual(value.FillColor, 0xe9, 0xec, 0xf0) && backgroundIndex < 0 {
				backgroundIndex = index
			}
		case creator.Text:
			// mergeRuns 会把行内代码后的空白并进前一段，故按前缀匹配。
			if strings.HasPrefix(value.Value, "Run()") {
				textIndex = index
			}
		}
	}
	if backgroundIndex < 0 {
		t.Fatal("行内代码缺少底色矩形")
	}
	if textIndex < 0 {
		t.Fatal("行内代码文字缺失")
	}
	if backgroundIndex > textIndex {
		t.Fatalf("底色应先于文字绘制，实际底色序号 %d、文字序号 %d", backgroundIndex, textIndex)
	}
}

// 行内代码底色应覆盖文字宽度并向上下各留出内边距。
func TestBuildInlineCodeBackgroundCoversText(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "调用 "}, {Text: "Run()", Code: true}, {Text: " 完成"}},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var background *creator.Path
	var text *creator.Text
	for _, item := range document.Pages[0].Items {
		switch value := item.(type) {
		case creator.Path:
			if colorEqual(value.FillColor, 0xe9, 0xec, 0xf0) {
				copied := value
				background = &copied
			}
		case creator.Text:
			if strings.HasPrefix(value.Value, "Run()") {
				copied := value
				text = &copied
			}
		}
	}
	if background == nil || text == nil {
		t.Fatalf("行内代码底色或文字缺失: %v %v", background != nil, text != nil)
	}
	// 底色只覆盖 glue 段本身，不含 mergeRuns 并入的尾随空白。
	codeWidth := measureWidth("Run()", text.Size, metricKey{mono: true})
	if background.X > text.X-options.InlineCodePaddingX+0.01 {
		t.Fatalf("底色左边 %.3f 未覆盖文字并留出内边距", background.X)
	}
	if background.X+background.Width < text.X+codeWidth+options.InlineCodePaddingX-0.01 {
		t.Fatalf("底色右边 %.3f 未覆盖代码文字并留出内边距", background.X+background.Width)
	}
	// 上下沿以基线为基准断言，不与文字框（Text.Height 直接取字号）比较。
	//
	// 底色高度是 ascent+descent+2×内边距，上下沿分别落在基线之上 ascent+内边距、
	// 之下 descent+内边距处；而文字框高度只是字号，两者口径本就不同。要求底色
	// 罩住整个文字框等于要求 ascent+内边距 ≥ 字号，这个比值随系统等宽字体变化：
	// 本机 Noto/Consolas 约 0.94 成立，CI 上约 0.83 不成立（底色顶边反而比文字框
	// 顶边低 0.3mm），但字形墨迹并没有超出底色——真正的墨迹范围就是
	// ascent+descent，底色按它绘制才是正确口径。
	//
	// 因此改为断言底色覆盖基线两侧的完整墨迹范围并各留出内边距，与字体无关。
	key := metricKey{mono: true}
	size := text.Size
	baseline := text.Y + text.Size // Text.Y 是文字框顶边，加字号到底边即基线
	aboveBaseline := ascent(size, key) + options.InlineCodePaddingY
	belowBaseline := descent(size, key) + options.InlineCodePaddingY
	if background.Y > baseline-aboveBaseline+0.01 {
		t.Fatalf("底色顶边 %.3f 未覆盖基线之上的墨迹与内边距（应在 %.3f 以内）", background.Y, baseline-aboveBaseline)
	}
	if background.Y+background.Height < baseline+belowBaseline-0.01 {
		t.Fatalf("底色底边 %.3f 未覆盖基线之下的墨迹与内边距（应在 %.3f 以外）", background.Y+background.Height, baseline+belowBaseline)
	}
}

// codePanels 收集全部页面上的代码块背景。
func codePanels(document *creator.Document) []creator.Path {
	var panels []creator.Path
	for _, page := range document.Pages {
		panels = append(panels, codePanelsOf(page)...)
	}
	return panels
}

func codePanelsOf(page creator.Page) []creator.Path {
	var panels []creator.Path
	for _, item := range page.Items {
		if path, ok := item.(creator.Path); ok && colorEqual(path.FillColor, 244, 245, 247) {
			panels = append(panels, path)
		}
	}
	return panels
}

func TestBuildLetterheadRendersOnFirstPage(t *testing.T) {
	document, err := Build(&Document{
		Title:      "公文",
		Letterhead: &Letterhead{Org: "××省档案局文件", DocNo: "×档发〔2026〕1号"},
		Blocks:     []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文内容"}}}},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var redTexts []string
	docNoCount := 0
	redPathCount := 0
	for _, page := range document.Pages {
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Text:
				switch value.Value {
				case "××省档案局文件":
					if !colorEqual(value.FillColor, 0xcc, 0x00, 0x00) {
						t.Fatalf("机关标志颜色 %v 不是红色", value.FillColor)
					}
					redTexts = append(redTexts, value.Value)
				case "×档发〔2026〕1号":
					if colorEqual(value.FillColor, 0xcc, 0x00, 0x00) {
						t.Fatalf("发文字号不应使用红色")
					}
					docNoCount++
				}
			case creator.Path:
				if colorEqual(value.FillColor, 0xcc, 0x00, 0x00) {
					redPathCount++
				}
			}
		}
	}
	if len(redTexts) != 1 {
		t.Fatalf("机关标志应以红色长文渲染 %d 次", len(redTexts))
	}
	if docNoCount != 1 {
		t.Fatalf("发文字号渲染 %d 次，应为 1", docNoCount)
	}
	if redPathCount == 0 {
		t.Fatalf("缺少红色分隔线")
	}
}

func TestBuildLetterheadOnlyOnFirstPage(t *testing.T) {
	options := DefaultOptions()
	options.PageHeight = 100
	blocks := make([]Block, 0, 60)
	for index := 0; index < 60; index++ {
		blocks = append(blocks, Block{Kind: KindParagraph, Inlines: []Inline{{Text: "正文段落内容"}}})
	}
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件"},
		Blocks:     blocks,
	}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) < 2 {
		t.Fatalf("应分页，实际 %d", len(document.Pages))
	}
	for index, page := range document.Pages {
		count := 0
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Text:
				if value.Value == "××省档案局文件" {
					count++
				}
			case creator.Path:
				if colorEqual(value.FillColor, 0xcc, 0x00, 0x00) {
					count++
				}
			}
		}
		if index == 0 && count == 0 {
			t.Fatalf("首页缺少版头")
		}
		if index > 0 && count != 0 {
			t.Fatalf("第 %d 页不应出现版头", index+1)
		}
	}
}

func TestBuildLetterheadSignatoryParallel(t *testing.T) {
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件", DocNo: "×档发〔2026〕1号", Signatory: "张三"},
		Blocks:     []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文"}}}},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var docX, docBase, signX, signBase, signWidth float64
	foundDoc, foundSign := false, false
	for _, page := range document.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			switch {
			case text.Value == "×档发〔2026〕1号":
				docX, docBase = text.X, text.Y+text.Height
				foundDoc = true
			case strings.HasPrefix(text.Value, "签发人"):
				signX, signBase, signWidth = text.X, text.Y+text.Height, text.Width
				foundSign = true
			}
		}
	}
	if !foundDoc || !foundSign {
		t.Fatalf("发文字号或签发人缺失: doc=%v sign=%v", foundDoc, foundSign)
	}
	if docBase != signBase {
		t.Fatalf("发文字号与签发人基线不一致: %.4f != %.4f", docBase, signBase)
	}
	if docX >= signX {
		t.Fatalf("发文字号应位于签发人左侧: %.4f >= %.4f", docX, signX)
	}
	if signX+signWidth > 184.99 {
		t.Fatalf("签发人应右空一字未超出版心右侧: x=%.4f w=%.4f", signX, signWidth)
	}
}

func TestBuildPageNumber(t *testing.T) {
	options := DefaultOptions()
	options.PageHeight = 100
	blocks := make([]Block, 0, 60)
	for index := 0; index < 60; index++ {
		blocks = append(blocks, Block{Kind: KindParagraph, Inlines: []Inline{{Text: "正文段落内容"}}})
	}
	document, err := Build(&Document{
		Footer: &Footer{PageNumber: true},
		Blocks: blocks,
	}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) < 2 {
		t.Fatalf("应分页，实际 %d", len(document.Pages))
	}
	halfway := options.PageWidth / 2
	for index, page := range document.Pages {
		var labels []string
		var xs []float64
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && strings.HasPrefix(text.Value, "— ") && strings.HasSuffix(text.Value, " —") {
				labels = append(labels, text.Value)
				xs = append(xs, text.X)
			}
		}
		if len(labels) != 1 {
			t.Fatalf("第 %d 页页码数量 %d，应为 1", index+1, len(labels))
		}
		if !strings.Contains(labels[0], fmt.Sprintf("%d", index+1)) {
			t.Fatalf("第 %d 页页码内容 %q", index+1, labels[0])
		}
		if index%2 == 0 && xs[0] < halfway {
			t.Fatalf("奇数页页码应居右: x=%.4f", xs[0])
		}
		if index%2 == 1 && xs[0] >= halfway {
			t.Fatalf("偶数页页码应居左: x=%.4f", xs[0])
		}
	}
}

func TestBuildLetterheadIssueMarks(t *testing.T) {
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件", SerialNo: "000018", Security: "内部", Urgency: "特急"},
		Blocks:     []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文"}}}},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	want := []string{"000018", "内部", "特急"}
	var marks []creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			for _, label := range want {
				if text.Value == label {
					if colorEqual(text.FillColor, 0xcc, 0x00, 0x00) {
						t.Fatalf("密级类标记不应使用红色: %q", label)
					}
					marks = append(marks, text)
				}
			}
		}
	}
	if len(marks) != len(want) {
		t.Fatalf("涉密类标记数量 %d，应为 %d", len(marks), len(want))
	}
	baseline := func(value creator.Text) float64 { return value.Y + value.Height }
	if !(baseline(marks[0]) < baseline(marks[1]) && baseline(marks[1]) < baseline(marks[2])) {
		t.Fatalf("份号/密级/紧急程度未按从上到下排列")
	}
	orgFound := false
	var orgBase float64
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && text.Value == "××省档案局文件" {
				orgFound = true
				orgBase = baseline(text)
			}
		}
	}
	if !orgFound || orgBase <= baseline(marks[2]) {
		t.Fatalf("机关标志应低于涉密类标记: org=%.4f mark=%.4f", orgBase, baseline(marks[2]))
	}
}

func TestBuildLetterheadDocNoAboveRedLine(t *testing.T) {
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件", DocNo: "×档发〔2026〕1号", Signatory: "张三"},
		Blocks:     []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文"}}}},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var docBottom, lineTop float64
	foundDoc, foundLine := false, false
	for _, page := range document.Pages {
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Text:
				if value.Value == "×档发〔2026〕1号" {
					docBottom = value.Y + value.Height
					foundDoc = true
				}
			case creator.Path:
				if colorEqual(value.FillColor, 0xcc, 0x00, 0x00) {
					lineTop = value.Y
					foundLine = true
				}
			}
		}
	}
	if !foundDoc || !foundLine {
		t.Fatalf("发文字号或红线缺失: doc=%v line=%v", foundDoc, foundLine)
	}
	if docBottom >= lineTop {
		t.Fatalf("发文字号与红线重叠: 文字底部 %.4f >= 红线上沿 %.4f", docBottom, lineTop)
	}
	if lineTop-docBottom < 3 {
		t.Fatalf("发文字号与红线间距过小: %.4f mm", lineTop-docBottom)
	}
}

func TestBuildOfficialTitleCenteredTwoPlain(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件", DocNo: "×档发〔2026〕1号"},
		Blocks: []Block{
			{Kind: KindHeading, Level: 1, Inlines: []Inline{{Text: "关于加强档案安全工作的通知"}}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "各市、县档案局："}}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "正文第一段。"}}},
		},
	}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var title *creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && text.Value == "关于加强档案安全工作的通知" {
				title = &text
			}
		}
	}
	if title == nil {
		t.Fatalf("缺少标题")
	}
	wantSize := ptToMM(22)
	if title.Height < wantSize-0.01 || title.Height > wantSize+0.01 {
		t.Fatalf("标题应使用二号（22pt），实际高度 %.4f", title.Height)
	}
	wantX := options.MarginLeft + (options.PageWidth-options.MarginLeft-options.MarginRight-title.Width)/2
	if title.X < wantX-0.2 || title.X > wantX+0.2 {
		t.Fatalf("标题应居中排布: x=%.4f 期望≈%.4f", title.X, wantX)
	}
	if title.X < options.MarginLeft || title.X+title.Width > options.PageWidth-options.MarginRight {
		t.Fatalf("标题应位于版心内: x=%.4f w=%.4f", title.X, title.Width)
	}
}

func TestBuildOfficialBodyIndentTwoChars(t *testing.T) {
	options := DefaultOptions()
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件"},
		Blocks: []Block{
			{Kind: KindHeading, Level: 1, Inlines: []Inline{{Text: "文件标题"}}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "各市、县档案局："}}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "这是一段较长的正文，用来验证回行后顶格、首行左空二字的排版。"}}},
		},
	}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	contentLeft := options.MarginLeft
	indent := 2 * ptToMM(options.BodySize)
	var bodyX, addresseeX *float64
	for _, page := range document.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			switch {
			case strings.Contains(text.Value, "各市"):
				x := text.X
				addresseeX = &x
			case strings.Contains(text.Value, "正文，用来验证"):
				x := text.X
				bodyX = &x
			}
		}
	}
	if addresseeX == nil {
		t.Fatalf("主送机关行缺失")
	}
	if *addresseeX < contentLeft-0.01 || *addresseeX > contentLeft+0.01 {
		t.Fatalf("主送机关行应顶格编排: x=%.4f", *addresseeX)
	}
	if bodyX == nil {
		t.Fatalf("正文行缺失")
	}
	if *bodyX < contentLeft+indent-0.2 || *bodyX > contentLeft+indent+0.2 {
		t.Fatalf("正文首行应左空二字: x=%.4f 期望≈%.4f", *bodyX, contentLeft+indent)
	}
}

func TestBuildSignatureRightAligned(t *testing.T) {
	document, err := Build(&Document{
		Sign:   &Signature{Org: "××省档案局", Date: "2026年9月19日"},
		Blocks: []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文"}}}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var orgText, dateText *creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				switch text.Value {
				case "××省档案局":
					orgText = &text
				case "2026年9月19日":
					dateText = &text
				}
			}
		}
	}
	if orgText == nil || dateText == nil {
		t.Fatalf("落款缺失: org=%v date=%v", orgText != nil, dateText != nil)
	}
	orgBase := orgText.Y + orgText.Height
	dateBase := dateText.Y + dateText.Height
	if dateBase <= orgBase {
		t.Fatalf("成文日期应位于署名下一行: %0.4f <= %.4f", dateBase, orgBase)
	}
	shift := dateText.X - orgText.X
	if shift < ptToMM(16)*2-0.1 || shift > ptToMM(16)*2+0.1 {
		t.Fatalf("日期首字应比署名首字右移二字: %.4f", shift)
	}
	if orgText.X+orgText.Width > 189.01 || dateText.X+dateText.Width > 189.01 {
		t.Fatalf("落款应右空编排并位于版心内")
	}
}

func TestBuildOfficialPageNumberBelowContent(t *testing.T) {
	options := DefaultOptions()
	options.PageHeight = 150
	document, err := Build(&Document{
		Letterhead: &Letterhead{Org: "××省档案局文件"},
		Footer:     &Footer{PageNumber: true},
		Blocks:     []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文内容"}}}},
	}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var pageText *creator.Text
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && strings.HasPrefix(text.Value, "— ") {
				pageText = &text
			}
		}
	}
	if pageText == nil {
		t.Fatalf("缺少页码")
	}
	// 页码应位于版心下边缘之下（一字线上距版心下边缘 7mm）。
	wantTop := options.PageHeight - options.MarginBottom + 7
	if pageText.Y < wantTop-0.5 || pageText.Y > wantTop+0.5 {
		t.Fatalf("页码应距版心下边缘 7mm: Y=%.4f 期望≈%.4f", pageText.Y, wantTop)
	}
}

func TestQuoteSeparatedFromFollowingTable(t *testing.T) {
	document, err := Build(&Document{
		Blocks: []Block{
			{Kind: KindQuote, Inlines: []Inline{{Text: "引用文本"}}},
			{Kind: KindTable, Table: &Table{
				Header: []Cell{{{Text: "名称"}}, {{Text: "数量"}}},
				Rows:   [][]Cell{{{{Text: "苹果"}}, {{Text: "3"}}}},
			}},
		},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	quoteBottom := 0.0
	headerTop := 0.0
	foundQuote, foundHeader := false, false
	for _, page := range document.Pages {
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Text:
				if value.FillColor == colorQuote {
					quoteBottom = value.Y + value.Height
					foundQuote = true
				}
			case creator.Path:
				if value.FillColor == colorHeaderBG && (!foundHeader || value.Y < headerTop) {
					headerTop = value.Y
					foundHeader = true
				}
			}
		}
	}
	if !foundQuote || !foundHeader {
		t.Fatalf("缺少引用或表格: quote=%v header=%v", foundQuote, foundHeader)
	}
	gap := headerTop - quoteBottom
	if gap < 7 {
		t.Fatalf("引用和表格间距过小: %.2fmm", gap)
	}
}

func TestBuildOfficialFontFamilies(t *testing.T) {
	document, err := Build(&Document{
		Letterhead: &Letterhead{
			Org:       "××省档案局文件",
			DocNo:     "×档发〔2026〕1号",
			Signatory: "李四",
			SerialNo:  "000018",
			Security:  "内部",
			Urgency:   "特急",
		},
		Footer: &Footer{PageNumber: true},
		Blocks: []Block{
			{Kind: KindHeading, Level: 1, Inlines: []Inline{{Text: "文件标题"}}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "各市、县档案局："}}},
			{Kind: KindHeading, Level: 2, Inlines: []Inline{{Text: "一、提高认识"}}},
			{Kind: KindHeading, Level: 3, Inlines: []Inline{{Text: "（一）加强领导"}}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "正文段落。"}}},
		},
	}, GBTOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	families := make(map[string]bool)
	for _, font := range document.Fonts {
		families[font.FamilyName] = true
	}
	// 正文/发文字号/冒号用仿宋、标题与机关标志用小标宋、序号与密级用黑体、
	// 签发人姓名与（一）层序号用楷体、页码用宋体。
	for _, name := range []string{"FangSong", "STZhongsong", "SimHei", "KaiTi", "SimSun"} {
		if !families[name] {
			t.Fatalf("缺少字体族 %s，实际 %v", name, families)
		}
	}
}

func TestBuildOfficialMarkFonts(t *testing.T) {
	document, err := Build(&Document{
		Letterhead: &Letterhead{
			Org:      "××省档案局文件",
			SerialNo: "000018",
			Security: "内部",
		},
		Blocks: []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文"}}}},
	}, GBTOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	familyByFont := make(map[string]string)
	for _, font := range document.Fonts {
		familyByFont[font.Name] = font.FamilyName
	}
	for _, page := range document.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			switch text.Value {
			case "000018":
				if got := familyByFont[text.Font]; got != "FangSong" {
					t.Fatalf("份号应使用仿宋: %q", got)
				}
			case "内部":
				if got := familyByFont[text.Font]; got != "SimHei" {
					t.Fatalf("密级应使用黑体: %q", got)
				}
			case "××省档案局文件":
				if got := familyByFont[text.Font]; got != "STZhongsong" {
					t.Fatalf("机关标志应使用小标宋: %q", got)
				}
			}
		}
	}
}

func TestBuildInlineItalicSetsItalicFlag(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "斜体abc", Italic: true}},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	foundItalicText := false
	for _, text := range textItems(document) {
		if strings.Contains(text.Value, "斜体") && text.Italic {
			foundItalicText = true
		}
	}
	if !foundItalicText {
		t.Fatalf("行内斜体未设置 Text.Italic")
	}
	foundItalicFont := false
	for _, font := range document.Fonts {
		if font.Italic {
			foundItalicFont = true
		}
	}
	if !foundItalicFont {
		t.Fatalf("未生成 Italic 字体资源")
	}
}

func TestBuildStrikeDrawsStrikethroughLine(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "删除线abc", Strike: true}},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	var target *creator.Text
	for _, text := range textItems(document) {
		if strings.Contains(text.Value, "删除线") {
			copied := text
			target = &copied
		}
	}
	if target == nil {
		t.Fatalf("缺少删除线文字")
	}
	inside := false
	for _, page := range document.Pages {
		for _, item := range page.Items {
			path, ok := item.(creator.Path)
			if !ok || !colorEqual(path.FillColor, 0x1f, 0x23, 0x28) {
				continue
			}
			if path.Height <= 0 || path.Height > target.Height {
				continue
			}
			if path.Y >= target.Y && path.Y+path.Height <= target.Y+target.Height {
				inside = true
			}
		}
	}
	if !inside {
		t.Fatalf("未在删除线文字中部找到删除线矩形")
	}
}

func TestBuildColophonDateOnlyKeepsDateInsideRules(t *testing.T) {
	document, err := Build(&Document{
		Colophon: &Colophon{IssuedDate: "2026年9月19日"},
		Blocks:   []Block{{Kind: KindParagraph, Inlines: []Inline{{Text: "正文"}}}},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	last := document.Pages[len(document.Pages)-1]
	var date *creator.Text
	var lines []creator.Path
	for _, item := range last.Items {
		switch value := item.(type) {
		case creator.Text:
			if strings.HasSuffix(value.Value, "印发") {
				copied := value
				date = &copied
			}
		case creator.Path:
			if colorEqual(value.FillColor, 0xc4, 0xcb, 0xd1) {
				lines = append(lines, value)
			}
		}
	}
	if date == nil {
		t.Fatalf("缺少印发日期")
	}
	if len(lines) < 2 {
		t.Fatalf("版记分隔线不足: %d", len(lines))
	}
	dateBottom := date.Y + date.Height
	below := false
	bottom := 0.0
	for _, line := range lines {
		if line.Y+line.Height > bottom {
			bottom = line.Y + line.Height
		}
		if line.Y >= dateBottom {
			below = true
		}
	}
	if !below {
		t.Fatalf("末条分隔线应在印发日期下方")
	}
	if bottom < 276.5 || bottom > 277.01 {
		t.Fatalf("版记下边缘未压准版心下边缘: %.4f", bottom)
	}
}

func TestBuildPartialOptionsKeepPageSize(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind:    KindParagraph,
		Inlines: []Inline{{Text: "正文"}},
	}}}, Options{PageWidth: 100, PageHeight: 120, MarginLeft: 5, MarginRight: 5, MarginTop: 5, MarginBottom: 5})
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if document.PageSize.Width != 100 || document.PageSize.Height != 120 {
		t.Fatalf("缺省字号时不应重置页面尺寸: %+v", document.PageSize)
	}
}

func TestBuildColophonOnLastPage(t *testing.T) {
	options := DefaultOptions()
	options.PageHeight = 100
	blocks := make([]Block, 0, 60)
	for index := 0; index < 60; index++ {
		blocks = append(blocks, Block{Kind: KindParagraph, Inlines: []Inline{{Text: "正文段落内容"}}})
	}
	document, err := Build(&Document{
		Colophon: &Colophon{Cc: "省委办公厅，省政府办公厅。", IssuedBy: "××省档案局办公室", IssuedDate: "2026年9月19日"},
		Blocks:   blocks,
	}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(document.Pages) < 2 {
		t.Fatalf("应分页，实际 %d", len(document.Pages))
	}
	last := document.Pages[len(document.Pages)-1]
	var cc, issuedBy, issuedDate *creator.Text
	var separatorPaths int
	for _, item := range last.Items {
		switch value := item.(type) {
		case creator.Text:
			switch {
			case strings.HasPrefix(value.Value, "抄送："):
				cc = &value
			case value.Value == "××省档案局办公室":
				issuedBy = &value
			case strings.HasSuffix(value.Value, "印发"):
				issuedDate = &value
			}
		case creator.Path:
			if colorEqual(value.FillColor, 0xc4, 0xcb, 0xd1) {
				separatorPaths++
			}
		}
	}
	if cc == nil || issuedBy == nil || issuedDate == nil {
		t.Fatalf("版记要素缺失: cc=%v by=%v date=%v", cc != nil, issuedBy != nil, issuedDate != nil)
	}
	if separatorPaths < 2 {
		t.Fatalf("版记分隔线数量 %d，应至少 2 条", separatorPaths)
	}
	if cc.Y >= issuedBy.Y {
		t.Fatalf("抄送应位于印发行上方: cc=%.4f print=%.4f", cc.Y, issuedBy.Y)
	}
	if issuedBy.Y != issuedDate.Y {
		t.Fatalf("印发机关与印发日期应同行: %.4f != %.4f", issuedBy.Y, issuedDate.Y)
	}
	if issuedDate.X <= issuedBy.X {
		t.Fatalf("印发日期应位于印发机关右侧: %0.4f <= %.4f", issuedDate.X, issuedBy.X)
	}
}

func TestBuildWideTableRotatesLandscape(t *testing.T) {
	options := DefaultOptions()
	rows := 6
	header := make([]Cell, 0, rows)
	body := make([]Cell, 0, rows)
	for index := 0; index < rows; index++ {
		header = append(header, Cell{{Text: "一二三四五六七八九"}})
		body = append(body, Cell{{Text: "甲乙丙丁戊己庚辛"}})
	}
	document, err := Build(&Document{Blocks: []Block{{
		Kind:  KindTable,
		Table: &Table{Header: header, Rows: [][]Cell{body}},
	}}}, options)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	contentLeft := options.MarginLeft
	var headerText, bodyText *creator.Text
	rotated := 0
	for _, page := range document.Pages {
		for _, item := range page.Items {
			text, ok := item.(creator.Text)
			if !ok {
				continue
			}
			if text.CharDirection != 0 {
				rotated++
			}
			switch {
			case text.Value == "一":
				headerText = &text
			case text.Value == "甲":
				bodyText = &text
			}
		}
	}
	if rotated == 0 {
		t.Fatalf("宽表格应横排旋转，未发现带方向角度的文字")
	}
	if headerText == nil || headerText.CharDirection != 270 {
		t.Fatalf("表头应横向旋转: header=%v", headerText)
	}
	if bodyText == nil || bodyText.CharDirection != 270 {
		t.Fatalf("数据行应横向旋转: body=%v", bodyText)
	}
	// 表头（首行）应落在页面左侧，数据行在其右侧；坐标已是直接换算的页面坐标。
	headerPageX := headerText.X + headerText.Height
	bodyPageX := bodyText.X + bodyText.Height
	if headerPageX < contentLeft-10 || headerPageX > contentLeft+12 {
		t.Fatalf("表头应在版心左侧: pageX=%.4f contentLeft=%.4f", headerPageX, contentLeft)
	}
	if bodyPageX <= headerPageX+1 {
		t.Fatalf("数据行应位于表头右侧: header=%.4f body=%.4f", headerPageX, bodyPageX)
	}
	if bodyPageX > options.PageHeight {
		t.Fatalf("数据行超出页面高度: %.4f", bodyPageX)
	}
}

func TestBuildNarrowTableStaysPortrait(t *testing.T) {
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
	for _, page := range document.Pages {
		for _, item := range page.Items {
			switch value := item.(type) {
			case creator.Text:
				if value.CharDirection != 0 {
					t.Fatalf("窄表格不应旋转: %q", value.Value)
				}
			case creator.Path:
				if value.CTM != nil {
					t.Fatalf("窄表格的路径不应旋转")
				}
			}
		}
	}
}

func TestBuildLandscapeForcedByFlag(t *testing.T) {
	document, err := Build(&Document{Blocks: []Block{{
		Kind: KindTable,
		Table: &Table{
			Header:    []Cell{{{Text: "名称"}}, {{Text: "数量"}}},
			Rows:      [][]Cell{{{{Text: "苹果"}}, {{Text: "3"}}}},
			Landscape: true,
		},
	}}}, DefaultOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	rotated := false
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && text.Value == "名" && text.CharDirection == 270 {
				rotated = true
			}
		}
	}
	if !rotated {
		t.Fatalf("显式 Landscape 的窄表格也应旋转")
	}
}

func TestBuildLandscapeTableKeepsPageNumber(t *testing.T) {
	document, err := Build(&Document{
		Footer:     &Footer{PageNumber: true},
		Letterhead: &Letterhead{Org: "××省档案局文件", DocNo: "×档发〔2026〕1号"},
		Blocks: []Block{
			{Kind: KindTable,
				Table: &Table{
					Header:    []Cell{{{Text: "名称"}}, {{Text: "数量"}}},
					Rows:      [][]Cell{{{{Text: "苹果"}}, {{Text: "3"}}}},
					Landscape: true,
				}},
			{Kind: KindParagraph, Inlines: []Inline{{Text: "正文。"}}},
		},
	}, GBTOptions())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	foundNumber := false
	for _, page := range document.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && strings.HasPrefix(text.Value, "— ") && strings.HasSuffix(text.Value, " —") {
				if text.CTM != nil {
					t.Fatalf("页码不应旋转: %q", text.Value)
				}
				foundNumber = true
			}
		}
	}
	if !foundNumber {
		t.Fatalf("横排表格页面仍应按公文惯例渲染页码")
	}
}

// TestBuildNoBlankPageBetweenFullHeightLandscapeTables 保护两个连续"按版心高等比
// 缩放"的横排表格之间不产生空白页。横排表格所需的纵向空间按构造等于整个版心
// 高，ensureHeight 又要多留一个字身的高度，因此它在一张刚由 newPage 建出的
// 页上一开始就放不下；此时若把 newPage 自己写下的页码算作"本页已有内容"，
// 就会多换一页并留下一张只有页码的空白页。
func TestBuildNoBlankPageBetweenFullHeightLandscapeTables(t *testing.T) {
	for _, testCase := range []struct {
		name string
		opts Options
		cols int
		cell string
	}{
		{"公文版式", GBTOptions(), 22, "列A"},
		{"默认版式", DefaultOptions(), 22, "内容内容"},
		{"宽表多列", DefaultOptions(), 30, "内容"},
		{"窄表长文本", DefaultOptions(), 16, "内容内容内容内容"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			wideTable := func() *Table {
				row := make([]Cell, 0, testCase.cols)
				for index := 0; index < testCase.cols; index++ {
					row = append(row, Cell{{Text: testCase.cell}})
				}
				return &Table{Header: row, Rows: [][]Cell{row}, Landscape: true}
			}
			document, err := Build(&Document{
				Footer: &Footer{PageNumber: true, Size: 14},
				Blocks: []Block{
					{Kind: KindParagraph, Inlines: []Inline{{Text: "中间段落"}}},
					{Kind: KindTable, Table: wideTable()},
					{Kind: KindTable, Table: wideTable()},
				},
			}, testCase.opts)
			if err != nil {
				t.Fatalf("Build 失败: %v", err)
			}
			if len(document.Pages) != 3 {
				t.Fatalf("两张横排表格应占 3 页，实际 %d 页", len(document.Pages))
			}
			for index, page := range document.Pages {
				var texts []creator.Text
				for _, item := range page.Items {
					if text, ok := item.(creator.Text); ok {
						texts = append(texts, text)
					}
				}
				if len(texts) == 1 && strings.HasPrefix(texts[0].Value, "— ") {
					t.Errorf("第 %d 页只有页码，是空白页", index+1)
				}
			}
		})
	}
}

// TestMeasureWidthStableAcrossBufferReuse 保护度量缓冲的池化复用。sfnt.Buffer
// 的内部缓冲会跨度量调用累积，缓冲被复用后同一段文本的宽度必须逐位相同，否则
// 断行位置会随排版顺序漂移。
func TestMeasureWidthStableAcrossBufferReuse(t *testing.T) {
	samples := []string{
		"这是一段中文文本，用于测量宽度稳定性。",
		"Hello ASCII text 123",
		"混合 mixed 文本 with 中文 and 数字 42",
		"标点，。、；：！？（）《》",
		"\uE000 私用区字符",
	}
	keys := []metricKey{
		{fam: famBody},
		{fam: famBody, bold: true},
		{fam: famTitle},
		{fam: famSong, mono: true},
		{fam: famHei},
	}
	sizes := []float64{3.5, 5.6, 7.4, 10.5, 21.6}
	first := make(map[string]float64)
	for round := 0; round < 4; round++ {
		for _, text := range samples {
			for _, key := range keys {
				for _, size := range sizes {
					got := measureWidth(text, size, key)
					id := fmt.Sprintf("%s|%v|%v", text, key, size)
					if round == 0 {
						first[id] = got
						continue
					}
					if got != first[id] {
						t.Fatalf("度量结果随缓冲复用变化 %s：第 %d 轮 %.12f != %.12f", id, round, got, first[id])
					}
				}
			}
		}
	}
}

// TestInlineCodeBackgroundIndependentOfFontMetrics 守住行内代码底色的上下沿断言
// 与系统等宽字体的 ascent/descent 比例无关。
//
// 底色按 ascent+descent+2×内边距绘制，字形墨迹范围正是 ascent+descent；但文字框
// 的 Height 直接取字号。要求底色罩住整个文字框等于要求
// ascent+内边距 ≥ 字号，这个比值随字体而变——本机约 0.94 成立，某些 CI 镜像约
// 0.83 不成立，测试便在无代码变更时因换机器而失败。这里用一组跨度很大的度量
// 重跑同一断言，确认底色始终贴着基线两侧的墨迹范围。
func TestInlineCodeBackgroundIndependentOfFontMetrics(t *testing.T) {
	metricsOnce.Do(initMetrics)
	key := metricKey{mono: true}
	original := metricAsc[key]
	defer func() { metricAsc[key] = original }()

	for _, ratio := range []float64{0.70, 0.8327, 0.9448, 1.10} {
		metricAsc[key] = ratio
		options := DefaultOptions()
		document, err := Build(&Document{Blocks: []Block{{
			Kind:    KindParagraph,
			Inlines: []Inline{{Text: "调用 "}, {Text: "Run()", Code: true}, {Text: " 完成"}},
		}}}, options)
		if err != nil {
			t.Fatalf("ascent 比例 %.4f: Build 失败: %v", ratio, err)
		}
		var background *creator.Path
		var text *creator.Text
		for _, item := range document.Pages[0].Items {
			switch value := item.(type) {
			case creator.Path:
				if colorEqual(value.FillColor, 0xe9, 0xec, 0xf0) {
					copied := value
					background = &copied
				}
			case creator.Text:
				if strings.HasPrefix(value.Value, "Run()") {
					copied := value
					text = &copied
				}
			}
		}
		if background == nil || text == nil {
			t.Fatalf("ascent 比例 %.4f: 行内代码底色或文字缺失", ratio)
		}
		baseline := text.Y + text.Size
		above := ascent(text.Size, key) + options.InlineCodePaddingY
		below := descent(text.Size, key) + options.InlineCodePaddingY
		if background.Y > baseline-above+0.01 {
			t.Errorf("ascent 比例 %.4f: 底色顶边 %.3f 未覆盖基线之上的墨迹", ratio, background.Y)
		}
		if background.Y+background.Height < baseline+below-0.01 {
			t.Errorf("ascent 比例 %.4f: 底色底边 %.3f 未覆盖基线之下的墨迹", ratio, background.Y+background.Height)
		}
		// 底色左右仍须覆盖代码文字并留出内边距。
		codeWidth := measureWidth("Run()", text.Size, key)
		if background.X > text.X-options.InlineCodePaddingX+0.01 {
			t.Errorf("ascent 比例 %.4f: 底色左边 %.3f 未覆盖文字", ratio, background.X)
		}
		if background.X+background.Width < text.X+codeWidth+options.InlineCodePaddingX-0.01 {
			t.Errorf("ascent 比例 %.4f: 底色右边 %.3f 未覆盖代码文字", ratio, background.X+background.Width)
		}
	}
}
