package invoice

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/textdoc"
	"github.com/zc310/ofd/pkg/converter"
)

// 拼版与裁页的取值。
const (
	// FitContain 等比缩放并居中补白，是唯一支持的适配方式。
	//
	// 不提供"填满裁切"：裁切会切掉票面边缘的印章与价税合计，对发票来说等于毁票。
	FitContain = "contain"
	// PagesAll 保留每张票的全部页面。
	PagesAll = "all"
	// PagesFirst 只取每张票的第 1 页。
	PagesFirst = "first"
	// PagesFace 只取每张票的票面页（页面上有发票代码或价税合计字样）。
	PagesFace = "face"
	// OrientationAuto 按实际票源挑占用率更高的方向，是默认值。
	OrientationAuto = "auto"
	// OrientationPortrait 固定纵向。
	OrientationPortrait = "portrait"
	// OrientationLandscape 固定横向，格子宽高随纸张一起交换。
	OrientationLandscape = "landscape"
)

// nupGrids 是 --nup 取值到「列数 × 行数」的映射，按纵向排布。
//
// 只给常用档位，不做「任意 N 自动排布」：按约定给死比让程序自己猜更可控，命令行
// 的含义也稳定。
//
// 两格与三格是**单列纵向拼接**而不是横向并排，因为发票里占比最大的是宽而矮的版式
// （卷式发票、行程单、车票）：A4 纵向单列时每格 210×148.5，210×140 的票按 1:1 铺满，
// 上下只留几毫米；改成横向两联的话每格只有 148.5 宽，票要被缩到 70%，纸面上留下
// 一大片空白。从 4 格起改成多列，否则单列会把票压到 50% 以下。
var nupGrids = map[int][2]int{
	1: {1, 1},
	2: {1, 2},
	3: {1, 3},
	4: {2, 2},
	6: {2, 3},
	8: {2, 4},
	9: {3, 3},
}

// sheetLayout 是一张输出纸的版面。
type sheetLayout struct {
	// width、height 是纸张尺寸（毫米）；fitSource 时按每页自身尺寸出纸，不用这两个值。
	width, height float64
	// columns、rows 是拼版格数。
	columns, rows int
	// gap 是相邻格子之间的间距（毫米）。
	gap float64
	// border 为真时给每个格子描一圈细线，方便打印后裁切。
	border bool
	// fitSource 为真时按源页原始尺寸出纸（未指定纸张且不拼版时的默认行为）。
	fitSource bool
}

// cell 返回第 index 个格子的外框（毫米，原点在纸张左上角）。
func (l sheetLayout) cell(index int) (x, y, width, height float64) {
	cellWidth := (l.width - float64(l.columns-1)*l.gap) / float64(l.columns)
	cellHeight := (l.height - float64(l.rows-1)*l.gap) / float64(l.rows)
	return float64(index%l.columns) * (cellWidth + l.gap),
		float64(index/l.columns) * (cellHeight + l.gap),
		cellWidth, cellHeight
}

// capacity 返回一张纸能放多少页源页面。
func (l sheetLayout) capacity() int {
	return l.columns * l.rows
}

// resolveSheetLayout 依据选项与实际票源尺寸算出输出版面。
//
// pages 给出各源页面的尺寸（毫米），用于 auto 方向判断占用率；为空时按 A4 纵向
// 处理。
//
// 默认组合有三种：什么都不给就逐页原样出纸（与旧行为一致）；只给 nup 就用 A4 并
// 自动挑方向；给了 paper 就按该纸张补白或拼版。
func resolveSheetLayout(options MergeOptions, pages []pageSize) (sheetLayout, error) {
	if options.NUp <= 0 {
		// 零值即"不拼版"：MergeOptions 是公开结构体，调用方常直接用字面量构造，
		// 把 0 当 1 处理比报错更符合直觉。
		options.NUp = 1
	}
	switch fit := strings.TrimSpace(options.Fit); fit {
	case "", FitContain:
	default:
		return sheetLayout{}, fmt.Errorf("不支持的适配方式 %q；目前只有 %s（等比缩放居中补白）",
			fit, FitContain)
	}
	grid, ok := nupGrids[options.NUp]
	if !ok {
		return sheetLayout{}, fmt.Errorf("不支持的拼版份数 %d，可用 %s", options.NUp, joinNupKeys())
	}
	if options.NUpGap < 0 {
		return sheetLayout{}, fmt.Errorf("拼版间距不能为负数")
	}
	// 纸张与方向先校验再决定是否走"按源页尺寸"分支：拼写错的方向不该因为
	// 没指定纸张就被静默忽略。
	if _, err := paperSize(options.Paper); err != nil {
		return sheetLayout{}, err
	}
	if _, err := resolveOrientation(options.Orientation); err != nil {
		return sheetLayout{}, err
	}
	layout := sheetLayout{
		columns: grid[0],
		rows:    grid[1],
		gap:     options.NUpGap,
		border:  options.CellBorder,
	}
	if strings.TrimSpace(options.Paper) == "" && options.NUp == 1 {
		layout.fitSource = true
		return layout, nil
	}
	landscape, err := resolveAutoOrientation(options.Orientation, options.Paper, layout, pages)
	if err != nil {
		return sheetLayout{}, err
	}
	width, height, err := resolvePaper(options.Paper, landscape)
	if err != nil {
		return sheetLayout{}, err
	}
	layout.width, layout.height = width, height
	return layout, nil
}

// pageSize 是一个源页面的尺寸（毫米）。
type pageSize struct {
	width, height float64
}

// resolveAutoOrientation 决定纸张是否横向。
//
// auto（默认）比较纵向与横向的**占用率**——源页面等比缩放后占格子面积的比例，
// 取高者。规则由票源长宽比与格子长宽比是否匹配决定，不由格子行列数决定：
// 210×140 的卷式票在 A4 纵向两格（格子 210×148.5）下能 1:1 铺满，占用率 94%；
// 同样两个格子排成横向（297×105）就只能缩到 75%，占用率 53%。而四格 2×2 时结论
// 相反：纵向格子 105×148.5 与票源差得太远（47%），横向格子 148.5×105 几乎同比例
// （94%）。所以同一个票源，2 格与 4 格的最优方向并不相同。
//
// 平局取纵向：结果与输入次序无关，重复运行给出一致输出。
func resolveAutoOrientation(orientation, paper string, layout sheetLayout,
	pages []pageSize) (bool, error) {
	switch strings.TrimSpace(orientation) {
	case OrientationLandscape:
		return true, nil
	case OrientationPortrait:
		return false, nil
	}
	size, err := paperSize(paper)
	if err != nil {
		return false, err
	}
	portrait := sheetLayout{width: size[0], height: size[1], columns: layout.columns, rows: layout.rows, gap: layout.gap}
	landscape := sheetLayout{width: size[1], height: size[0], columns: layout.columns, rows: layout.rows, gap: layout.gap}
	return fillRatio(landscape, pages) > fillRatio(portrait, pages), nil
}

// fillRatio 返回一批页面在给定版面上的平均占用率（0~1）。
//
// 没有页面尺寸时返回 0，两个方向打平，交给调用方按纵向处理。
func fillRatio(layout sheetLayout, pages []pageSize) float64 {
	if len(pages) == 0 || layout.columns <= 0 || layout.rows <= 0 {
		return 0
	}
	_, _, cellWidth, cellHeight := layout.cell(0)
	if cellWidth <= 0 || cellHeight <= 0 {
		return 0
	}
	cell := cellWidth * cellHeight
	total := 0.0
	counted := 0
	for _, page := range pages {
		if page.width <= 0 || page.height <= 0 {
			continue
		}
		scale := min(cellWidth/page.width, cellHeight/page.height, 1.0)
		total += (page.width * scale) * (page.height * scale) / cell
		counted++
	}
	if counted == 0 {
		return 0
	}
	return total / float64(counted)
}

// paperSize 返回命名纸张的尺寸（毫米）。空名称返回 A4：拼版没给纸张时用 A4，
// 发票本身就是 A4 版式，换别的纸没有依据。
func paperSize(paper string) ([2]float64, error) {
	if strings.TrimSpace(paper) == "" {
		return converter.NamedPaperSizes["A4"], nil
	}
	named, err := converter.PaperByName(paper)
	if err != nil {
		return [2]float64{}, err
	}
	return [2]float64{named.Width, named.Height}, nil
}

// resolveOrientation 校验方向取值。auto 的判定依赖票源尺寸，由
// resolveAutoOrientation 处理，这里只做取值校验。
func resolveOrientation(orientation string) (bool, error) {
	switch strings.TrimSpace(orientation) {
	case "", OrientationAuto:
		return false, nil
	case OrientationLandscape:
		return true, nil
	case OrientationPortrait:
		return false, nil
	default:
		return false, fmt.Errorf("不支持的方向 %q，可用 %s、%s、%s",
			orientation, OrientationAuto, OrientationPortrait, OrientationLandscape)
	}
}

// resolvePaper 返回输出纸张尺寸（毫米），已考虑方向。
func resolvePaper(paper string, landscape bool) (float64, float64, error) {
	size, err := paperSize(paper)
	if err != nil {
		return 0, 0, err
	}
	if landscape {
		return size[1], size[0], nil
	}
	return size[0], size[1], nil
}

// joinNupKeys 返回可用的拼版份数，供错误信息使用。
func joinNupKeys() string {
	keys := make([]int, 0, len(nupGrids))
	for key := range nupGrids {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strconv.Itoa(key))
	}
	return strings.Join(parts, "、")
}

// sheetEntry 是输出 PDF 里的一页源页面。
type sheetEntry struct {
	document *render.Document
	page     *parser.Page
	// width、height 是源页尺寸（毫米），用于等比缩放。
	width, height float64
	// sheet 是这页落在第几张纸上（从 0 起），index 是格子下标。
	sheet int
	index int
	// title 是这张纸的大纲标题，空串表示不写大纲。
	title string
	// fullSheet 为真时这张页面独占一张纸，不参与拼版格位。汇总对账表走这条：
	// 它是给整张纸看的内容，压进九宫格的一个格子里就没人读了。
	fullSheet bool
}

// canvasOrigin 把「自纸张上边缘起算」的落位换算成绘制上下文的平移量。
//
// 矢量表面与 PDF 的 y 轴自纸张**下**边缘起算、向上为正，而版面计算用的是自上而下的
// 格子坐标，两者差一次翻转。不换算的话第一个格子会落到纸张下半部分：单格单页看
// 不出来（格子恰好与纸张等高，翻转后又落回原处），一格多票时整份票序就是颠倒的。
func canvasOrigin(sheetHeight, top, height float64) float64 {
	return sheetHeight - top - height
}

// entryPageSizes 取各源页面的尺寸，供 auto 方向判断占用率。
func entryPageSizes(entries []sheetEntry) []pageSize {
	sizes := make([]pageSize, 0, len(entries))
	for _, entry := range entries {
		sizes = append(sizes, pageSize{width: entry.width, height: entry.height})
	}
	return sizes
}

// fitTransform 计算把 width×height 的源页放进 x/y/w/h 格子的等比缩放与位移。
//
// 只做「等比缩小 + 居中补白」，**不放 大**：票面的物理尺寸本身就是信息（定额发票
// 的票面大小、火车票的票幅），放大后打印件与票面对不上，扫码、核销都可能出问题。
// 源页比格子小的时候按 1:1 印，多出来的部分留白。
//
// 也不提供填满裁切：票面的印章和价税合计都在边缘，裁切等于毁票。
func fitTransform(sourceWidth, sourceHeight, x, y, width, height float64) (scale, dx, dy float64) {
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return 1, x, y
	}
	scale = min(width/sourceWidth, height/sourceHeight, 1.0)
	return scale, x + (width-sourceWidth*scale)/2, y + (height-sourceHeight*scale)/2
}

// normalizePageSpec 校验并归一 --pages 的取值。
func normalizePageSpec(spec string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(spec))
	switch value {
	case "":
		return PagesAll, nil
	case PagesAll, PagesFirst, PagesFace:
		return value, nil
	default:
		if _, err := parsePageNumbers(value); err != nil {
			return "", fmt.Errorf("不支持的取页方式 %q，可用 %s、%s、%s 或页码表达式（如 1,3-5）",
				spec, PagesAll, PagesFirst, PagesFace)
		}
		return value, nil
	}
}

// selectPages 按 --pages 从一个文档体里挑出要输出的页面。
//
// 挑不出一页时报错：静默输出零页会让整批结果少一张票，而调用方看不到任何提示。
func selectPages(doc *parser.Document, spec string) ([]*parser.Page, error) {
	if doc == nil {
		return nil, fmt.Errorf("文档体为空")
	}
	all := make([]*parser.Page, 0, len(doc.Pages))
	for _, page := range doc.Pages {
		if page != nil {
			all = append(all, page)
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("文档没有页面")
	}
	switch spec {
	case "", PagesAll:
		return all, nil
	case PagesFirst:
		return all[:1], nil
	case PagesFace:
		faces, err := facePages(doc)
		if err != nil {
			return nil, err
		}
		if len(faces) > 0 {
			return faces, nil
		}
		// 认不出票面页时退回首页：猜不出来就什么都不输出是最糟的结果。
		return all[:1], nil
	default:
		numbers, err := parsePageNumbers(spec)
		if err != nil {
			return nil, err
		}
		picked := make([]*parser.Page, 0, len(numbers))
		for _, number := range numbers {
			if number < 1 || number > len(all) {
				return nil, fmt.Errorf("页码 %d 超出范围（该票共 %d 页）", number, len(all))
			}
			picked = append(picked, all[number-1])
		}
		return picked, nil
	}
}

// faceKeywords 是票面页的文字特征。发票清单页只有商品明细，不含这些字样。
var faceKeywords = []string{"发票代码", "发票号码", "价税合计", "开票日期"}

// facePages 找出票面页：页面上出现发票代码、发票号码、价税合计或开票日期字样。
//
// OFD 没有「哪一页是票面」的标记，只能看文字层。返回全部命中的页（正常票只有
// 一页）；认不出时返回空切片，由调用方决定回退。
func facePages(doc *parser.Document) ([]*parser.Page, error) {
	if doc == nil || len(doc.Pages) == 0 {
		return nil, nil
	}
	collected, err := textdoc.Collect(context.Background(), []*parser.Document{doc}, 0, len(doc.Pages))
	if err != nil {
		return nil, err
	}
	var faces []*parser.Page
	for _, page := range collected {
		if page.Source == nil {
			continue
		}
		for _, entry := range page.Entries {
			if containsAnyKeyword(entry.Text) {
				faces = append(faces, page.Source)
				break
			}
		}
	}
	return faces, nil
}

// containsAnyKeyword 判断文字是否含票面特征词。
func containsAnyKeyword(text string) bool {
	for _, keyword := range faceKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// parsePageNumbers 解析 "1,3-5" 形式的页码表达式，返回从 1 起的页序。
//
// 与 pkg/creator/merge.ParsePages 同义，这里单独实现是为了不把 pkg/creator/merge 整套
// （creator、manifest、export）拖进只做发票抽取的包。
func parsePageNumbers(spec string) ([]int, error) {
	var pages []int
	for _, token := range strings.Split(spec, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, fmt.Errorf("页码列表为空: %q", spec)
		}
		if start, end, ok := strings.Cut(token, "-"); ok {
			from, err := strconv.Atoi(strings.TrimSpace(start))
			if err != nil || from < 1 {
				return nil, fmt.Errorf("页码无效: %q", token)
			}
			to, err := strconv.Atoi(strings.TrimSpace(end))
			if err != nil || to < from {
				return nil, fmt.Errorf("页码区间无效: %q", token)
			}
			for page := from; page <= to; page++ {
				pages = append(pages, page)
			}
			continue
		}
		page, err := strconv.Atoi(token)
		if err != nil || page < 1 {
			return nil, fmt.Errorf("页码无效: %q", token)
		}
		pages = append(pages, page)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("页码列表为空: %q", spec)
	}
	return pages, nil
}
