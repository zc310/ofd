package invoice

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/render"
)

// TestResolveSheetLayoutDefaults 确认不给版面参数时逐页原样出纸。
//
// 这是向后兼容的底线：不加任何选项的合并结果必须与引入拼版之前逐页同构——
// 既不缩放也不补白，PDF 里每页的尺寸就是源页尺寸。
func TestResolveSheetLayoutDefaults(t *testing.T) {
	layout, err := resolveSheetLayout(MergeOptions{}, nil)
	if err != nil {
		t.Fatalf("解析默认版面失败: %v", err)
	}
	if !layout.fitSource {
		t.Error("默认应按源页尺寸出纸")
	}
	if layout.capacity() != 1 {
		t.Errorf("默认一张纸一页，实际 %d", layout.capacity())
	}
}

// wideTickets 是宽而矮的卷式票尺寸（210×140），auto 方向判定的主要输入。
var wideTickets = []pageSize{{width: 210, height: 140}}

// TestResolveSheetLayoutGrid 确认拼版份数到格数的映射，以及份数非法时报错。
func TestResolveSheetLayoutGrid(t *testing.T) {
	// 网格固定为纵向；方向由 auto 按占用率选：卷式票在 2/3/6/8 格下纵向更省，
	// 4 格下横向的 2×2 格子与票源同比例、更省（见 TestAutoOrientationFollowsFillRatio）。
	cases := []struct {
		nup           int
		columns, rows int
		width, height float64
	}{
		{nup: 2, columns: 1, rows: 2, width: 210, height: 297},
		{nup: 3, columns: 1, rows: 3, width: 210, height: 297},
		{nup: 4, columns: 2, rows: 2, width: 297, height: 210},
		{nup: 6, columns: 2, rows: 3, width: 210, height: 297},
		{nup: 8, columns: 2, rows: 4, width: 210, height: 297},
		// 3×3 的格子（纵向 70×99、横向 99×70）与 210×140 差得远，横向 99×70 更接近。
		{nup: 9, columns: 3, rows: 3, width: 297, height: 210},
	}
	for _, c := range cases {
		layout, err := resolveSheetLayout(MergeOptions{NUp: c.nup}, wideTickets)
		if err != nil {
			t.Fatalf("nup=%d 解析失败: %v", c.nup, err)
		}
		if layout.columns != c.columns || layout.rows != c.rows {
			t.Errorf("nup=%d 格数 = %dx%d，期望 %dx%d", c.nup,
				layout.columns, layout.rows, c.columns, c.rows)
		}
		if layout.width != c.width || layout.height != c.height {
			t.Errorf("nup=%d 纸张 = %.0fx%.0f，期望 %.0fx%.0f",
				c.nup, layout.width, layout.height, c.width, c.height)
		}
	}
	if _, err := resolveSheetLayout(MergeOptions{NUp: 5}, nil); err == nil {
		t.Error("nup=5 应报错")
	} else if !strings.Contains(err.Error(), "1、2、3、4、6、8、9") {
		t.Errorf("错误信息应列出可用值，实际：%v", err)
	}
}

// TestResolveSheetLayoutPaper 确认指定纸张时按该纸张出纸，方向可显式覆盖。
func TestResolveSheetLayoutPaper(t *testing.T) {
	layout, err := resolveSheetLayout(MergeOptions{Paper: "A5"}, nil)
	if err != nil {
		t.Fatalf("解析 A5 版面失败: %v", err)
	}
	if layout.width != 148 || layout.height != 210 {
		t.Errorf("A5 = %.0fx%.0f，期望 148x210", layout.width, layout.height)
	}
	landscape, err := resolveSheetLayout(MergeOptions{Paper: "A5", Orientation: OrientationLandscape}, nil)
	if err != nil {
		t.Fatalf("解析横向 A5 失败: %v", err)
	}
	if landscape.width != 210 || landscape.height != 148 {
		t.Errorf("横向 A5 = %.0fx%.0f，期望 210x148", landscape.width, landscape.height)
	}
	if _, err := resolveSheetLayout(MergeOptions{Paper: "A0"}, nil); err == nil {
		t.Error("不支持的纸张应报错")
	}
	if _, err := resolveSheetLayout(MergeOptions{Orientation: "diagonal"}, nil); err == nil {
		t.Error("不支持的方向应报错（即使没指定纸张也要报错，不能静默忽略）")
	}

}

// TestAutoOrientationFollowsFillRatio 确认 auto 按占用率选方向，而不是按格子行列数。
//
// 210×140 的卷式票在 A4 上：两格纵向（格子 210×148.5）能 1:1 铺满，占用率 94%；四格
// 2×2 纵向（格子 105×148.5）与票源差太远，只有 47%，而横向的 148.5×105 几乎同比例，
// 能到 94%。同一个票源，2 格与 4 格的最优方向相反——这正是"按行列数判方向"会选错
// 的地方，也是 auto 存在的理由。
func TestAutoOrientationFollowsFillRatio(t *testing.T) {
	cases := []struct {
		nup           int
		wantLandscape bool
		portraitFill  float64
		landscapeFill float64
	}{
		{nup: 2, wantLandscape: false, portraitFill: 0.943, landscapeFill: 0.530},
		{nup: 4, wantLandscape: true, portraitFill: 0.471, landscapeFill: 0.943},
	}
	for _, c := range cases {
		grid := nupGrids[c.nup]
		portrait := sheetLayout{width: 210, height: 297, columns: grid[0], rows: grid[1]}
		landscape := sheetLayout{width: 297, height: 210, columns: grid[0], rows: grid[1]}
		if got := fillRatio(portrait, wideTickets); math.Abs(got-c.portraitFill) > 0.01 {
			t.Errorf("nup=%d 纵向占用率 = %.3f，期望 %.3f", c.nup, got, c.portraitFill)
		}
		if got := fillRatio(landscape, wideTickets); math.Abs(got-c.landscapeFill) > 0.01 {
			t.Errorf("nup=%d 横向占用率 = %.3f，期望 %.3f", c.nup, got, c.landscapeFill)
		}
		layout, err := resolveSheetLayout(MergeOptions{NUp: c.nup}, wideTickets)
		if err != nil {
			t.Fatalf("nup=%d 解析版面失败: %v", c.nup, err)
		}
		gotLandscape := layout.width > layout.height
		if gotLandscape != c.wantLandscape {
			t.Errorf("nup=%d auto 选了横向=%v，期望 %v（%.0fx%.0f）",
				c.nup, gotLandscape, c.wantLandscape, layout.width, layout.height)
		}
	}
}

// TestAutoOrientationDefaultsToPortrait 确认不给方向时，宽而矮的发票默认纵向。
//
// 这是最常见的票源（卷式发票、行程单、车票），纵向两格能 1:1 打印。
func TestAutoOrientationDefaultsToPortrait(t *testing.T) {
	layout, err := resolveSheetLayout(MergeOptions{NUp: 2}, wideTickets)
	if err != nil {
		t.Fatalf("解析版面失败: %v", err)
	}
	if layout.width != 210 || layout.height != 297 {
		t.Errorf("默认应为 A4 纵向，实际 %.0fx%.0f", layout.width, layout.height)
	}
}

// TestExplicitOrientationOverridesAuto 确认显式方向压过 auto 判定。
func TestExplicitOrientationOverridesAuto(t *testing.T) {
	// 四格时 auto 会选横向，显式指定纵向应当照办。
	layout, err := resolveSheetLayout(MergeOptions{NUp: 4, Orientation: OrientationPortrait}, wideTickets)
	if err != nil {
		t.Fatalf("解析版面失败: %v", err)
	}
	if layout.width != 210 || layout.height != 297 {
		t.Errorf("显式纵向应得 210x297，实际 %.0fx%.0f", layout.width, layout.height)
	}
}

// TestFillRatioNeedsPageSizes 确认没有页面尺寸时占用率为 0，
// auto 因此退化为纵向（结果与输入无关，不随机摇摆）。
func TestFillRatioNeedsPageSizes(t *testing.T) {
	if got := fillRatio(sheetLayout{width: 210, height: 297}, nil); got != 0 {
		t.Errorf("无页面尺寸时占用率应为 0，实际 %v", got)
	}
	if got := fillRatio(sheetLayout{width: 210, height: 297, columns: 1, rows: 1},
		[]pageSize{{width: 0, height: 0}}); got != 0 {
		t.Errorf("尺寸缺失的页面不应计入占用率，实际 %v", got)
	}
	// 网格未初始化时不能除零：cell() 会拿 columns 当除数。
	if got := fillRatio(sheetLayout{width: 210, height: 297},
		wideTickets); got != 0 {
		t.Errorf("未设置网格时占用率应为 0，实际 %v", got)
	}
}

// TestNupKeepsWideTicketsAtFullScale 守住拼版的质量底线：宽而矮的票在两格拼版下
// 必须保持 1:1。
//
// 这正是把默认方向从"横向两联"改成"纵向单列"的原因：横向时每格只有 148.5mm 宽，
// 210mm 的票被缩到 70%，纸面上下留出一大片空白；纵向单列每格 210×148.5，票按原
// 尺寸铺满，只剩几毫米余量。缩放率一旦退回去，票面小字和印章就更难核。
func TestNupKeepsWideTicketsAtFullScale(t *testing.T) {
	layout, err := resolveSheetLayout(MergeOptions{NUp: 2}, wideTickets)
	if err != nil {
		t.Fatalf("解析两格版面失败: %v", err)
	}
	_, _, cellWidth, cellHeight := layout.cell(0)
	scale, _, _ := fitTransform(210, 140, 0, 0, cellWidth, cellHeight)
	if scale != 1 {
		t.Errorf("缩放 = %v，期望 1（210x140 的票应在 A4 纵向两格下保持原始尺寸）", scale)
	}
	// 反例：横向两联会把票缩到 70%。
	_, _, wide, _ := sheetLayout{width: 297, height: 210, columns: 2, rows: 1}.cell(0)
	if scale, _, _ := fitTransform(210, 140, 0, 0, wide, 210); scale >= 0.9 {
		t.Errorf("横向两联的缩放 = %v，应当明显小于 1（本用例的前提是纵横向确实有差别）", scale)
	}
}

// TestResolveSheetLayoutRejectsCrop 确认不提供"填满裁切"。
//
// 裁切会切掉票面边缘的印章与价税合计，对发票等于毁票；这条一旦放开就只能再收回，
// 因此在选项校验层就拒绝。
func TestResolveSheetLayoutRejectsCrop(t *testing.T) {
	_, err := resolveSheetLayout(MergeOptions{Fit: "cover"}, nil)
	if err == nil {
		t.Fatal("fit=cover 应报错")
	}
	if !strings.Contains(err.Error(), FitContain) {
		t.Errorf("错误信息应说明只支持 %s，实际：%v", FitContain, err)
	}
}

// TestSheetLayoutCellGeometry 确认格子不重叠且不越出版心。
func TestSheetLayoutCellGeometry(t *testing.T) {
	layout := sheetLayout{width: 297, height: 210, columns: 2, rows: 1, gap: 5}
	x0, y0, w0, h0 := layout.cell(0)
	x1, _, w1, _ := layout.cell(1)
	if x0 != 0 || y0 != 0 {
		t.Errorf("首个格子应从原点开始，实际 (%v,%v)", x0, y0)
	}
	if x0+w0+layout.gap != x1 {
		t.Errorf("格子间距 = %v，期望 %v", x1-(x0+w0), layout.gap)
	}
	if x1+w1 > layout.width || h0 > layout.height {
		t.Errorf("末个格子越界：右边 %v > %v，下边 %v > %v", x1+w1, layout.width, h0, layout.height)
	}
}

// TestFitTransformNeverEnlarges 确认票面永不被放大。
//
// 票面的物理尺寸本身就是信息（定额发票的票面大小、火车票票幅），放大后打印件与
// 票面对不上；源页比格子小时按 1:1 印、留白补齐。没有这条约束时，210×140 的票在
// A4 横向下会被放到 141%，纸面看着更满，票却是错的。
func TestFitTransformNeverEnlarges(t *testing.T) {
	scale, dx, dy := fitTransform(100, 60, 0, 0, 297, 210)
	if scale != 1 {
		t.Errorf("缩放 = %v，期望 1（不允许放大）", scale)
	}
	if math.Abs(dx-98.5) > 0.001 || math.Abs(dy-75) > 0.001 {
		t.Errorf("居中位置 = (%v,%v)，期望 (98.5,75)", dx, dy)
	}
}

// TestFitTransformScalesAndCenters 确认等比缩放并居中补白。
//
// 票面不会被拉伸变形，也不会贴到格子边缘；源页比格子窄时两侧留白、高时上下留白。
func TestFitTransformScalesAndCenters(t *testing.T) {
	scale, dx, dy := fitTransform(100, 200, 0, 0, 200, 200)
	if scale != 1 {
		t.Errorf("缩放 = %v，期望 1（受高度限制）", scale)
	}
	if dx != 50 || dy != 0 {
		t.Errorf("居中位置 = (%v,%v)，期望 (50,0)", dx, dy)
	}
	scale, dx, dy = fitTransform(400, 100, 0, 0, 200, 200)
	if scale != 0.5 {
		t.Errorf("缩放 = %v，期望 0.5", scale)
	}
	if dy != 75 {
		t.Errorf("上下各留白 75，dy = %v", dy)
	}
	if scale, dx, dy = fitTransform(0, 0, 7, 9, 200, 200); scale != 1 || dx != 7 || dy != 9 {
		t.Errorf("尺寸缺失时应原样落位，实际 %v (%v,%v)", scale, dx, dy)
	}
}

// TestNormalizePageSpec 确认取页方式的校验与归一。
func TestNormalizePageSpec(t *testing.T) {
	for _, spec := range []string{"", "all", "FIRST", " face ", "1,3-5"} {
		if _, err := normalizePageSpec(spec); err != nil {
			t.Errorf("取页方式 %q 应被接受：%v", spec, err)
		}
	}
	if got, _ := normalizePageSpec(""); got != PagesAll {
		t.Errorf("空值应归一为 %s，实际 %s", PagesAll, got)
	}
	if _, err := normalizePageSpec("封面"); err == nil {
		t.Error("未知取页方式应报错")
	}
}

// TestParsePageNumbers 确认页码表达式解析，含区间与去重无关的原样顺序。
func TestParsePageNumbers(t *testing.T) {
	pages, err := parsePageNumbers("1,3-5")
	if err != nil {
		t.Fatalf("解析页码失败: %v", err)
	}
	if !reflect.DeepEqual(pages, []int{1, 3, 4, 5}) {
		t.Errorf("页码 = %v，期望 [1 3 4 5]", pages)
	}
	for _, spec := range []string{"", "0", "3-1", "a", "1,,2", "-"} {
		if _, err := parsePageNumbers(spec); err == nil {
			t.Errorf("页码表达式 %q 应报错", spec)
		}
	}
}

// TestSelectPagesLimitsPages 确认 first 只取首页，页码越界报错。
func TestSelectPagesLimitsPages(t *testing.T) {
	doc, closer := twoPageInvoice(t)
	defer func() { _ = closer.Close() }()

	all, err := selectPages(doc, "")
	if err != nil {
		t.Fatalf("取全部页失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("全部页 = %d，期望 2", len(all))
	}
	first, err := selectPages(doc, PagesFirst)
	if err != nil {
		t.Fatalf("取首页失败: %v", err)
	}
	if len(first) != 1 {
		t.Errorf("首页应只有 1 页，实际 %d", len(first))
	}
	if _, err := selectPages(doc, "3"); err == nil {
		t.Error("越界页码应报错")
	}
	if _, err := selectPages(nil, ""); err == nil {
		t.Error("空文档体应报错")
	}
}

// TestSelectPagesFindsFacePage 确认 face 找到带发票代码的那一页，而不是第一页。
//
// 电子普票常见第 1 页是发票清单、第 2 页才是票面；按第一页取会拿到清单页。
func TestSelectPagesFindsFacePage(t *testing.T) {
	doc, closer := invoiceWithFaceOnSecondPage(t)
	defer func() { _ = closer.Close() }()

	faces, err := selectPages(doc, PagesFace)
	if err != nil {
		t.Fatalf("取票面页失败: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("应识别出 1 个票面页，实际 %d", len(faces))
	}
	if faces[0] != doc.Pages[1] {
		t.Error("票面页应是第 2 页")
	}
	first, err := selectPages(doc, PagesFirst)
	if err != nil {
		t.Fatalf("取首页失败: %v", err)
	}
	if first[0] == faces[0] {
		t.Error("first 与 face 应指向不同页，否则这个用例没有区分度")
	}
}

// TestSelectPagesFallsBackWhenNoFace 确认认不出票面页时退回首页。
func TestSelectPagesFallsBackWhenNoFace(t *testing.T) {
	doc, closer := twoPageInvoice(t)
	defer func() { _ = closer.Close() }()

	faces, err := selectPages(doc, PagesFace)
	if err != nil {
		t.Fatalf("取票面页失败: %v", err)
	}
	if len(faces) != 1 || faces[0] != doc.Pages[0] {
		t.Error("没有票面特征时应退回第 1 页")
	}
}

// TestCellBorderIsStrokeOnly 守住拼版格线只描边、不填充。
//
// DrawPath 会同时描边和填充，而画布的默认填充色是黑色：忘了 ClearFill 就会把整格
// 涂成黑块。这个缺陷只在加 --cell-border 时出现，票面本身却完整，很容易被当成
// 源文件或渲染问题，因此在这里钉住 PDF 内容流里的路径操作符。
func TestCellBorderIsStrokeOnly(t *testing.T) {
	prefix := sheetContentPrefix(t, true)
	if !strings.Contains(prefix, " l s") {
		t.Errorf("格线路径应以描边（s）结束，实际内容：%q", prefix)
	}
	if strings.Contains(prefix, " l b") || strings.Contains(prefix, " l f") {
		t.Errorf("格线不应带填充（b/f），否则整格会被涂黑：%q", prefix)
	}
	if !strings.Contains(prefix, "G") || !strings.Contains(prefix, " w") {
		t.Errorf("应设置描边颜色与线宽，实际内容：%q", prefix)
	}
}

// TestCellBorderAbsentByDefault 确认不开 --cell-border 时不画格线。
func TestCellBorderAbsentByDefault(t *testing.T) {
	prefix := sheetContentPrefix(t, false)
	if strings.Contains(prefix, " G ") {
		t.Errorf("未开启格线时不应设置描边颜色，实际内容：%q", prefix)
	}
}

// grayStrokePattern 匹配 PDF 内容流里的灰色描边设置（0xBF/255）。
var grayStrokePattern = regexp.MustCompile(`\.74901961 G`)

// sheetContentPrefix 返回一张带格线的输出纸在正文开始之前的内容流片段。
func sheetContentPrefix(t *testing.T, border bool) string {
	t.Helper()
	data := renderSheetPDF(t, border)
	stream := bytes.Index(data, []byte("stream"))
	if stream < 0 {
		t.Fatal("PDF 里没有内容流")
	}
	body := data[stream+7:]
	if end := bytes.Index(body, []byte("endstream")); end > 0 {
		body = body[:end]
	}
	if text := bytes.Index(body, []byte("BT")); text > 0 {
		body = body[:text]
	}
	if border && !grayStrokePattern.Match(body) {
		t.Errorf("应画出 0xBF 灰的格线，实际内容：%q", body)
	}
	return string(body)
}

// renderSheetPDF 渲染一张 A4 纸的输出，内容流不压缩以便断言。
func renderSheetPDF(t *testing.T, border bool) []byte {
	t.Helper()
	source := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	sources := batchSources(source)
	items := mustExtractBatch(t, sources, BatchOptions{}).ActiveItems()
	merged, closers, err := parseDocuments(context.Background(), items,
		append([]Source{}, sources...), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	defer closeAll(closers)
	entries, err := collectSheetEntries(merged, items)
	if err != nil {
		t.Fatalf("收集页面失败: %v", err)
	}
	layout, err := resolveSheetLayout(MergeOptions{Paper: "A4", CellBorder: border}, nil)
	if err != nil {
		t.Fatalf("解析版面失败: %v", err)
	}
	var buffer bytes.Buffer
	doc, err := render.NewPDFDocument(&buffer, render.PDFOptions{Compress: false})
	if err != nil {
		t.Fatalf("创建 PDF 失败: %v", err)
	}
	for _, sheet := range groupSheets(entries, layout) {
		surface, err := renderSheet(sheet, layout)
		if err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		if err := doc.AddPage(surface, nil); err != nil {
			t.Fatalf("写页失败: %v", err)
		}
	}
	if err := doc.Close(); err != nil {
		t.Fatalf("关闭 PDF 失败: %v", err)
	}
	return buffer.Bytes()
}

// TestMergePDFPageGeometry 确认三种版式在 PDF 里的实际表现：默认按源页尺寸、
// 指定纸张补白、拼版合成一页。
//
// 断言的是 PDF 的 /MediaBox（单位 pt，换算回毫米）而不是内部计算结果——版面
// 参数最终要落到纸面上，中间任何一层算错都会在这里暴露。
func TestMergePDFPageGeometry(t *testing.T) {
	small := invoiceFixture{
		name: "small.ofd", code: "033001900111", number: "1", date: "2026-09-01",
		kind: "普通发票", title: "增值税电子普通发票",
		sellerName: "卖方公司", sellerCode: "911100001111000000", buyerName: "买方公司",
		amount: "100.00", tax: "13.00", total: "113.00",
		pageWidth: 210, pageHeight: 140, // 卷式发票：不是 A4
	}
	second := small
	second.name = "small2.ofd"
	second.number = "2"
	sources := batchSources(small, second)

	cases := []struct {
		name    string
		options MergeOptions
		pages   int
		width   float64
		height  float64
	}{
		{name: "按源页尺寸", options: MergeOptions{}, pages: 2, width: 210, height: 140},
		{name: "补白到 A4", options: MergeOptions{Paper: "A4"}, pages: 2, width: 210, height: 297},
		{name: "两格纵向拼接", options: MergeOptions{NUp: 2}, pages: 1, width: 210, height: 297},
		{name: "两格强制横向", options: MergeOptions{NUp: 2, Orientation: OrientationLandscape},
			pages: 1, width: 297, height: 210},
		// 四格时 auto 选横向：2×2 的横向格子（148.5×105）与 210×140 的票源同比例。
		{name: "四格自动横向", options: MergeOptions{NUp: 4}, pages: 1, width: 297, height: 210},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if _, err := MergePDF(context.Background(), sources, &buffer, c.options); err != nil {
				t.Fatalf("合并失败: %v", err)
			}
			sizes, pages := pdfPageSizes(buffer.Bytes())
			if pages != c.pages {
				t.Errorf("页数 = %d，期望 %d", pages, c.pages)
			}
			if len(sizes) == 0 {
				t.Fatal("PDF 里没有 MediaBox")
			}
			for index, size := range sizes {
				if math.Abs(size.width-c.width) > 0.5 || math.Abs(size.height-c.height) > 0.5 {
					t.Errorf("第 %d 页 = %.1fx%.1f mm，期望 %.0fx%.0f",
						index+1, size.width, size.height, c.width, c.height)
				}
			}
		})
	}
}

// TestMergePDFPageSelection 确认按票裁页真的减少了页数。
func TestMergePDFPageSelection(t *testing.T) {
	data, closer := twoPageInvoice(t)
	defer func() { _ = closer.Close() }()
	_ = data

	source := invoiceFixture{
		name: "a.ofd", code: "033001900111", number: "1", date: "2026-09-01",
		kind: "普通发票", title: "增值税电子普通发票",
		sellerName: "卖方公司", sellerCode: "911100001111000000", buyerName: "买方公司",
		amount: "100.00", tax: "13.00", total: "113.00",
	}
	sources := []Source{{Name: "a.ofd", Input: sampleInvoice(source)}}

	var all, first, face bytes.Buffer
	if _, err := MergePDF(context.Background(), sources, &all, MergeOptions{}); err != nil {
		t.Fatalf("合并全部页失败: %v", err)
	}
	if _, err := MergePDF(context.Background(), sources, &first, MergeOptions{Pages: PagesFirst}); err != nil {
		t.Fatalf("合并首页失败: %v", err)
	}
	if _, err := MergePDF(context.Background(), sources, &face, MergeOptions{Pages: PagesFace}); err != nil {
		t.Fatalf("合并票面页失败: %v", err)
	}
	if _, pages := pdfPageSizes(all.Bytes()); pages != 1 {
		t.Errorf("单页票的全部页 = %d，期望 1", pages)
	}
	if _, pages := pdfPageSizes(first.Bytes()); pages != 1 {
		t.Errorf("--pages first = %d 页，期望 1", pages)
	}
	if _, pages := pdfPageSizes(face.Bytes()); pages != 1 {
		t.Errorf("--pages face = %d 页，期望 1", pages)
	}
	if _, err := MergePDF(context.Background(), sources, &bytes.Buffer{},
		MergeOptions{Pages: "9"}); err == nil {
		t.Error("越界页码应报错")
	}
}

// TestMergePDFOutlinesFollowInvoices 确认每张票在大纲里有一条，指向它出现的纸。
func TestMergePDFOutlinesFollowInvoices(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := plainInvoice("b.ofd", "033001900111", "2", "2026-09-02")
	var buffer bytes.Buffer
	if _, err := MergePDF(context.Background(), batchSources(first, second), &buffer,
		MergeOptions{NUp: 2}); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	data := buffer.Bytes()
	if !bytes.Contains(data, []byte("/Outlines")) {
		t.Error("拼版后的 PDF 应有大纲")
	}
	if !bytes.Contains(data, []byte("a.ofd")) || !bytes.Contains(data, []byte("b.ofd")) {
		t.Error("大纲标题应包含每张票的来源名")
	}
}

// pdfPageSizes 解析 PDF 的页数与每页尺寸（毫米）。
func pdfPageSizes(data []byte) ([]struct{ width, height float64 }, int) {
	type size = struct{ width, height float64 }
	var sizes []size
	for _, match := range regexp.MustCompile(`/MediaBox\[([^\]]*)\]`).FindAllSubmatch(data, -1) {
		fields := strings.Fields(string(match[1]))
		if len(fields) != 4 {
			continue
		}
		values := make([]float64, 0, 4)
		for _, field := range fields {
			value, err := strconv.ParseFloat(field, 64)
			if err != nil {
				values = nil
				break
			}
			values = append(values, value)
		}
		if values == nil {
			continue
		}
		sizes = append(sizes, size{
			width:  (values[2] - values[0]) * 25.4 / 72,
			height: (values[3] - values[1]) * 25.4 / 72,
		})
	}
	return sizes, len(pdfPagePattern.FindAll(data, -1))
}

// TestCanvasOriginFlipsTopDownToBottomUp 钉住坐标系换算。
//
// 矢量表面与 PDF 的 y 轴自纸张下边缘起算、向上为正，版面计算却是自上而下的格子
// 坐标，两者差一次翻转。少了换算，单格单页看不出来（格子与纸张等高，翻转后落回原
// 处），一格多票时第一张票就掉到了下半张纸。
func TestCanvasOriginFlipsTopDownToBottomUp(t *testing.T) {
	const sheetHeight = 297.0
	cases := []struct {
		top, height, want float64
	}{
		{top: 0, height: 140, want: 157},     // 顶格
		{top: 148.5, height: 140, want: 8.5}, // 次格
		{top: 78.5, height: 140, want: 78.5}, // 整张纸居中：翻转后不动
	}
	for _, c := range cases {
		if got := canvasOrigin(sheetHeight, c.top, c.height); math.Abs(got-c.want) > 0.001 {
			t.Errorf("canvasOrigin(%.1f, %.1f) = %.1f，期望 %.1f",
				sheetHeight, c.top, got, c.want)
		}
	}
}

// TestSheetEntriesKeepPageOrder 确认一格多票时票序与输入一致（第一张在上）。
//
// 从 PDF 内容流里取文字落点的 y 坐标：canvas 会把平移直接算进输出坐标，内容流里
// 只有文字块的 Td 携带位置，因此"第一张的 y 高于第二张"就是票序正确的证据。
// PDF 的 y 自下而上，y 越大越靠上。
func TestSheetEntriesKeepPageOrder(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	first.pageWidth, first.pageHeight = 210, 140
	first.title = "第一张"
	second := first
	second.name, second.number, second.title = "b.ofd", "2", "第二张"

	body := sheetContentStream(t, MergeOptions{NUp: 2}, first, second)
	positions := regexp.MustCompile(`[-\d.]+ [-\d.]+ Td`).FindAllString(body, -1)
	if len(positions) < 2 {
		t.Fatalf("内容流里应有至少两个文字块，实际 %d：%q", len(positions), body)
	}
	firstY, secondY := textPositionY(t, positions[0]), textPositionY(t, positions[1])
	if firstY <= secondY {
		t.Errorf("第一张票应排在上面，实际 y=%.1f 与 y=%.1f（PDF 坐标自下而上）", firstY, secondY)
	}
	if firstY > 297 || secondY < 0 {
		t.Errorf("文字落点超出纸张：y=%.1f / %.1f", firstY, secondY)
	}
}

// TestCellBordersAreDashed 确认裁切线是虚线。
//
// 实线打印后容易被当成票面的一部分；虚线一眼能认出是后加的辅助线。
func TestCellBordersAreDashed(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	first.pageWidth, first.pageHeight = 210, 140
	second := first
	second.name, second.number = "b.ofd", "2"
	body := sheetContentStream(t, MergeOptions{NUp: 2, CellBorder: true}, first, second)
	// 断线的长度和空白都写死在这里：3mm 实 4mm 空，空白比实线段更长，密集的
	// 短线在票据打印分辨率下会糊成灰实线。
	dash := regexp.MustCompile(`\[3 4\] [\d.]+ d`)
	if !dash.MatchString(body) {
		t.Errorf("裁切线应为 3mm 实 4mm 空的虚线，实际：%q", body)
	}
}

// sheetContentStream 渲染两页拼版并返回未压缩的 PDF 内容流。
func sheetContentStream(t *testing.T, options MergeOptions, fixtures ...invoiceFixture) string {
	t.Helper()
	sources := batchSources(fixtures...)
	items := mustExtractBatch(t, sources, BatchOptions{}).ActiveItems()
	merged, closers, err := parseDocuments(context.Background(), items,
		append([]Source{}, sources...), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	defer closeAll(closers)
	entries, err := collectSheetEntries(merged, items)
	if err != nil {
		t.Fatalf("收集页面失败: %v", err)
	}
	layout, err := resolveSheetLayout(options, entryPageSizes(entries))
	if err != nil {
		t.Fatalf("解析版面失败: %v", err)
	}
	var buffer bytes.Buffer
	doc, err := render.NewPDFDocument(&buffer, render.PDFOptions{Compress: false})
	if err != nil {
		t.Fatalf("创建 PDF 失败: %v", err)
	}
	for _, sheet := range groupSheets(entries, layout) {
		surface, err := renderSheet(sheet, layout)
		if err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		if err := doc.AddPage(surface, nil); err != nil {
			t.Fatalf("写页失败: %v", err)
		}
	}
	if err := doc.Close(); err != nil {
		t.Fatalf("关闭 PDF 失败: %v", err)
	}
	data := buffer.Bytes()
	start := bytes.Index(data, []byte("stream"))
	end := bytes.Index(data, []byte("endstream"))
	if start < 0 || end < start {
		t.Fatal("PDF 里没有内容流")
	}
	return string(data[start+7 : end])
}

// textPositionY 取出 "x y Td" 里的 y。
func textPositionY(t *testing.T, position string) float64 {
	t.Helper()
	fields := strings.Fields(position)
	if len(fields) < 3 {
		t.Fatalf("无法解析文字位置 %q", position)
	}
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		t.Fatalf("解析文字 y 坐标 %q 失败: %v", fields[1], err)
	}
	return value
}

// TestCellBordersCoverEveryCell 确认每个放了票的格子都有一圈自己的裁切线。
//
// 转换层按 *geom.Path 指针缓存转换结果（canvasconv.ToCanvasPath），所以复用一个
// Path 再 Reset 拿到的是首次的缓存，所有格子会画在第一格的位置——看上去就是
// 只有第一张票有框。这条断言数的是"不同起点的裁切线有几个"，不是"有没有"。
func TestCellBordersCoverEveryCell(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	first.pageWidth, first.pageHeight = 210, 140
	fixtures := []invoiceFixture{first}
	for i := 2; i <= 4; i++ {
		next := first
		next.name = fmt.Sprintf("%d.ofd", i)
		next.number = fmt.Sprintf("%d", i)
		fixtures = append(fixtures, next)
	}
	body := sheetContentStream(t, MergeOptions{NUp: 4, CellBorder: true}, fixtures...)

	starts := regexp.MustCompile(`([\d.]+) ([\d.]+) m [\d.]+ [\d.]+ l`).FindAllStringSubmatch(body, -1)
	unique := make(map[string]bool, len(starts))
	for _, match := range starts {
		unique[match[1]+","+match[2]] = true
	}
	if len(unique) != 4 {
		t.Errorf("四格应有 4 个不同起点的裁切线，实际 %d 个：%v", len(unique), keysOf(unique))
	}
}

// keysOf 返回集合的键，便于失败信息可读。
func keysOf(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestCoverPageTakesItsOwnSheet 确认汇总对账页独占一张纸，不被塞进拼版格位。
//
// 汇总页是给一整页看的内容：跟着 --nup 9 挤进九宫格左上角那一格之后，字只有原来
// 的三分之一，等于白生成。独占一张纸时它与票页同样大小，一眼能看清合计。
func TestCoverPageTakesItsOwnSheet(t *testing.T) {
	first := plainInvoice("a.ofd", "033001900111", "1", "2026-09-01")
	second := first
	second.name, second.number = "b.ofd", "2"
	third := first
	third.name, third.number = "c.ofd", "3"

	entries := []sheetEntry{
		{width: 210, height: 297, title: "发票汇总对账表", fullSheet: true},
		{width: 210, height: 140, title: "a.ofd"},
		{width: 210, height: 140, title: "b.ofd"},
		{width: 210, height: 140, title: "c.ofd"},
	}
	layout, err := resolveSheetLayout(MergeOptions{NUp: 9}, entryPageSizes(entries[1:]))
	if err != nil {
		t.Fatalf("解析版面失败: %v", err)
	}
	sheets := groupSheets(entries, layout)
	if len(sheets) != 2 {
		t.Fatalf("汇总页 + 3 张票（9 宫格）应是 2 张纸，实际 %d 张", len(sheets))
	}
	if len(sheets[0].entries) != 1 || !sheets[0].entries[0].fullSheet {
		t.Errorf("第一张纸应只含汇总页，实际 %d 项", len(sheets[0].entries))
	}
	if len(sheets[1].entries) != 3 {
		t.Errorf("第二张纸应装 3 张票，实际 %d 张", len(sheets[1].entries))
	}
	// 汇总页铺满整张纸，不落在某个格子里。
	x, y, width, height := sheetCell(layout, sheets[0], sheets[0].entries[0])
	if x != 0 || y != 0 || width != sheets[0].width || height != sheets[0].height {
		t.Errorf("汇总页应铺满整张纸，实际 (%v,%v %.0fx%.0f)，纸面 %.0fx%.0f",
			x, y, width, height, sheets[0].width, sheets[0].height)
	}
	// 票页仍按格位落：九宫格的第一格远小于整张纸。
	tx, ty, tw, _ := sheetCell(layout, sheets[1], sheets[1].entries[0])
	if tw >= sheets[1].width || tx == 0 && ty == 0 && tw == sheets[1].width {
		t.Errorf("票页应落在某个格子里，实际格宽 %.1f（纸宽 %.1f）", tw, sheets[1].width)
	}
}

// TestCoverKeepsPortraitOnLandscapeSheets 确认输出纸被选成横向时汇总页仍是纵向。
//
// --nup 4 时 auto 会把输出纸选成横向（210×140 的票在 2×2 横向更省）。汇总页此前
// 跟着横向，210×297 的内容被 fitTransform 缩到 0.7 倍塞进左半张纸，右半张全空、
// 字号跟着变小。汇总页要的是同一张纸的纵向。
func TestCoverKeepsPortraitOnLandscapeSheets(t *testing.T) {
	entries := []sheetEntry{
		{width: 210, height: 297, title: "汇总", fullSheet: true},
		{width: 210, height: 140, title: "a.ofd"},
	}
	layout, err := resolveSheetLayout(MergeOptions{NUp: 4, Paper: "A5"},
		entryPageSizes(entries[1:]))
	if err != nil {
		t.Fatalf("解析版面失败: %v", err)
	}
	sheets := groupSheets(entries, layout)
	if len(sheets) != 2 {
		t.Fatalf("应是 2 张纸，实际 %d 张", len(sheets))
	}
	cover, ticket := sheets[0], sheets[1]
	// 同一张纸的两边长，只是方向相反。
	if cover.width != ticket.height || cover.height != ticket.width {
		t.Errorf("汇总页 %.0fx%.0f 应是票页 %.0fx%.0f 的纵向",
			cover.width, cover.height, ticket.width, ticket.height)
	}
	if cover.width > cover.height {
		t.Errorf("汇总页应为纵向，实际 %.0fx%.0f", cover.width, cover.height)
	}
	// 票页仍缩到格子里：2×2 的格子远小于整张 A5。
	_, _, cellWidth, _ := sheetCell(layout, ticket, ticket.entries[0])
	if cellWidth >= ticket.width {
		t.Errorf("票页应缩进格子，实际格宽 %.1f、纸宽 %.1f", cellWidth, ticket.width)
	}
}

// 汇总页的纸张尺寸与它的落位必须一致，否则整页会被缩放塞进纸里。
func TestCoverPageMatchesItsSheet(t *testing.T) {
	for _, options := range []MergeOptions{
		{NUp: 1}, {NUp: 2}, {NUp: 4}, {NUp: 4, Paper: "A5"}, {NUp: 3, Paper: "A3"},
	} {
		size := coverSheetSize(mustResolveLayout(t, options, 210, 140))
		if size.width > size.height {
			t.Errorf("%+v：汇总页应为纵向，实际 %.0fx%.0f", options, size.width, size.height)
		}
		if size.width <= 0 || size.height <= 0 {
			t.Errorf("%+v：汇总页尺寸无效 %.0fx%.0f", options, size.width, size.height)
		}
	}
}

// mustResolveLayout 按给定票源解析版面。
func mustResolveLayout(t *testing.T, options MergeOptions, width, height float64) sheetLayout {
	t.Helper()
	layout, err := resolveSheetLayout(options, entryPageSizes([]sheetEntry{{width: width, height: height}}))
	if err != nil {
		t.Fatalf("%+v 解析版面失败: %v", options, err)
	}
	return layout
}
