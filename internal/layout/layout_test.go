package layout

import (
	"fmt"
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
