package invoice

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/zc310/ofd/pkg/creator"
)

// 汇总页的排版常量，单位毫米。页面按 A4 竖排，版心左右各留 18mm。
const (
	coverPageWidth   = 210.0
	coverPageHeight  = 297.0
	coverMarginLeft  = 18.0
	coverMarginRight = 18.0
	coverMarginTop   = 20.0
	coverTitleSize   = 6.0
	coverTitleHeight = 12.0
	coverHeadSize    = 3.6
	coverBodySize    = 3.2
	coverRowHeight   = 5.2
	coverHeadHeight  = 6.0
	coverGap         = 3.0
	// coverLabelWidth 是行首项目列的宽度，数值列从它之后开始。列宽合计等于版心
	// 宽度（210 - 18 - 18 = 174mm），多出的项目名才有地方放。
	coverLabelWidth = 68.0
	// coverCountWidth 是张数列的宽度。
	coverCountWidth = 16.0
	// coverValueWidth 是每个金额列的宽度。
	coverValueWidth = 30.0
)

const (
	// creatorName 写进汇总页的 Creator 元数据。
	creatorName = "zc310/ofd ofd-invoice"
	// coverFont 是汇总页使用的逻辑字体名，只声明名称，由创建器子集化系统字体。
	coverFont = "楷体"
	// defaultCoverTitle 是汇总页的默认标题。
	defaultCoverTitle = "发票汇总对账表"
)

// coverHeader 是汇总页各张表共用的表头。
var coverHeader = []string{"项目", "张数", "不含税金额", "税额", "价税合计"}

// coverWidths 与 coverHeader 一一对应的列宽。
var coverWidths = []float64{coverLabelWidth, coverCountWidth, coverValueWidth, coverValueWidth, coverValueWidth}

// coverAlignRight 标记需要右对齐的列下标，让金额的小数点对齐。
var coverAlignRight = map[int]bool{1: true, 2: true, 3: true, 4: true}

// coverTable 是汇总页上的一张表：标题 + 表头 + 数据行。
type coverTable struct {
	title string
	rows  [][]string
}

// coverPageData 是一页汇总页的排版内容。
type coverPageData struct {
	title string
	// note 是表格上方的补充说明，没有表格时用于提示空结果。
	note string
	// tables 是按纵向顺序排布的表与其顶边位置。
	tables []coverTableLayout
}

// coverTableLayout 是一张表在某一页上的位置与内容。
type coverTableLayout struct {
	table coverTable
	top   float64
}

// coverPageProvider 按索引提供汇总页，是 creator.PageProvider 的实现。
type coverPageProvider struct {
	pages []creator.Page
}

func (p coverPageProvider) PageCount() int {
	return len(p.pages)
}

func (p coverPageProvider) PageAt(index int) (creator.Page, error) {
	if index < 0 || index >= len(p.pages) {
		return creator.Page{}, fmt.Errorf("汇总页页码越界: %d", index)
	}
	return p.pages[index], nil
}

// coverPages 把汇总结果排成分页的表格模型：合计、按销售方、按类型、按月份。
//
// 每一张表都完整求和后再逐页排布，跨页只切分数据行、不重复表头与标题，
// 这样同一份汇总无论打印多少页，每张分组表都只有一份合计。
func coverPages(summary Summary, title string) []coverPageData {
	if strings.TrimSpace(title) == "" {
		title = defaultCoverTitle
	}
	tables := []coverTable{{rows: [][]string{{
		fmt.Sprintf("发票合计 %d 张", summary.Count),
		fmt.Sprintf("%d", summary.Count),
		moneyText(summary.Amount),
		moneyText(summary.TaxAmount),
		moneyText(summary.TotalAmount),
	}}}}
	if summary.Red > 0 {
		tables[0].rows = append(tables[0].rows, []string{
			fmt.Sprintf("其中红字发票 %d 张", summary.Red), "", "", "", "",
		})
	}
	for _, group := range []struct {
		title  string
		groups []Group
	}{
		{"按销售方汇总", summary.BySeller},
		{"按发票类型汇总", summary.ByType},
		{"按开票月份汇总", summary.ByMonth},
	} {
		if len(group.groups) == 0 {
			continue
		}
		rows := make([][]string, 0, len(group.groups))
		for _, entry := range group.groups {
			// 项目列优先写「名称（税号）」；税号是分组键，对账时要看的是它。
			// 放不下时先去掉税号，再按列宽截断——挤进金额列会和数字糊在一起。
			label := entry.Name
			if entry.Key != "" && entry.Key != entry.Name {
				label = fmt.Sprintf("%s（%s）", entry.Name, entry.Key)
			}
			if textWidth(label, coverBodySize) > coverLabelWidth {
				label = ellipsize(label, coverBodySize, coverLabelWidth)
			}
			rows = append(rows, []string{
				label,
				fmt.Sprintf("%d", entry.Count),
				moneyText(entry.Amount),
				moneyText(entry.TaxAmount),
				moneyText(entry.TotalAmount),
			})
		}
		tables = append(tables, coverTable{title: group.title, rows: rows})
	}
	return paginateCover(title, tables)
}

// coverContentTop 是正文顶边：标题行（含行距）与口径说明行各占一行。
const coverContentTop = coverMarginTop + coverTitleHeight + coverHeadHeight

// paginateCover 按页面可用高度把各张表分页。
//
// 跨页只切分数据行：表头与表标题只在每页开头出现一次，合计始终只出现在明细
// 末尾的那一行，不会因为分页被重复计算。比一页还高的表按行切成多片，仍然不
// 丢行。
func paginateCover(title string, tables []coverTable) []coverPageData {
	var pages []coverPageData
	current := coverPageData{title: title}
	for _, table := range tables {
		rows := table.rows
		for len(rows) > 0 {
			room := coverPageHeight - coverMarginTop - coverContentTop - usedHeight(current)
			capacity := int(room / coverRowHeight)
			if capacity > len(rows) {
				capacity = len(rows)
			}
			if capacity < 1 {
				pages = append(pages, current)
				current = coverPageData{title: title}
				continue
			}
			piece := table
			piece.rows = rows[:capacity]
			rows = rows[capacity:]
			current.tables = append(current.tables, coverTableLayout{
				table: piece,
				top:   coverContentTop + usedHeight(current),
			})
		}
	}
	if len(current.tables) == 0 {
		current.note = "没有可汇总的发票"
	}
	pages = append(pages, current)
	return pages
}

// usedHeight 返回当前页已排入的表占用的总高度。
func usedHeight(data coverPageData) float64 {
	height := 0.0
	for _, layout := range data.tables {
		height += coverTableHeight(layout.table)
	}
	return height
}

// coverTableHeight 返回一张表（含标题与表头）占用的纵向高度。
func coverTableHeight(table coverTable) float64 {
	return coverGap + coverHeadHeight + coverRowHeight*float64(len(table.rows)+1)
}

// coverPage 把一页排版内容转成 creator 页面模型。
func coverPage(data coverPageData) creator.Page {
	var page creator.Page
	width := coverPageWidth - coverMarginLeft - coverMarginRight
	y := coverMarginTop
	page.Items = append(page.Items, coverText(coverMarginLeft, y, width, coverTitleSize, data.title, true))
	y += coverTitleHeight
	page.Items = append(page.Items, coverText(coverMarginLeft, y, width, coverHeadSize, coverNote, false))
	if data.note != "" {
		y += coverHeadHeight
		page.Items = append(page.Items, coverText(coverMarginLeft, y, width, coverBodySize, data.note, false))
	}
	for _, layout := range data.tables {
		table := layout.table
		top := layout.top
		if table.title != "" {
			page.Items = append(page.Items, coverText(coverMarginLeft, top, width, coverHeadSize, table.title, true))
		}
		row := top + coverHeadHeight
		page.Items = append(page.Items, coverRow(coverHeader, row, coverBodySize, true)...)
		row += coverRowHeight
		for _, values := range table.rows {
			page.Items = append(page.Items, coverRow(values, row, coverBodySize, false)...)
			row += coverRowHeight
		}
	}
	return page
}

// coverNote 是汇总页上固定出现的口径说明。
const coverNote = "金额单位：元；红字发票金额为负，已参与合计。"

// coverRow 返回一行表格的文字图元。
//
// 右对齐的列先按列宽减去文本宽度再落位，金额的小数点才能对齐；文本宽度用
// textWidth 估算，同一列字符种类固定，估算误差不会随行变化。
func coverRow(values []string, y, size float64, bold bool) []creator.Item {
	items := make([]creator.Item, 0, len(values))
	x := coverMarginLeft
	for index, value := range values {
		width := coverValueWidth
		if index < len(coverWidths) {
			width = coverWidths[index]
		}
		// 空单元格不生成图元：红字发票那行只有项目列有文字，其余列是占位空串。
		// creator 校验文字内容不能为空，生成了整份汇总页都创建失败。
		if value != "" {
			left := x
			// 表头同样右对齐：金额列表头左对齐、数值右对齐时两者会错开一整个列宽。
			if coverAlignRight[index] {
				left = x + width - textWidth(value, size)
			}
			items = append(items, coverText(left, y, width, size, value, bold))
		}
		x += width
	}
	return items
}

// coverText 构造一个文字图元。
func coverText(x, y, width, size float64, value string, bold bool) creator.Item {
	weight := 400
	if bold {
		weight = 700
	}
	return creator.Text{
		X:      x,
		Y:      y,
		Width:  width,
		Height: size * 1.4,
		Value:  value,
		Size:   size,
		Font:   coverFont,
		Weight: weight,
	}
}

// ellipsize 把文本截到给定宽度内，末尾用省略号标出被截掉的部分。
func ellipsize(text string, size, maxWidth float64) string {
	if textWidth(text, size) <= maxWidth {
		return text
	}
	var builder strings.Builder
	width := 0.0
	for _, r := range text {
		advance := textWidth(string(r), size)
		if width+advance > maxWidth-size {
			break
		}
		builder.WriteRune(r)
		width += advance
	}
	return builder.String() + "…"
}

// textWidth 估算文本在给定字号下的宽度，单位毫米。
//
// 汇总页按固定列宽排版，这里用「汉字与全角标点按一个字宽、ASCII 按半宽」的近似：
// 数字的推进宽度在绝大多数字体里都是半个字宽，金额列因此能对齐；项目列的名称
// 字数不定，但左对齐不依赖宽度估算。
func textWidth(text string, size float64) float64 {
	width := 0.0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Ideographic, r), isFullWidthPunctuation(r):
			width += size
		default:
			width += size * 0.5
		}
	}
	return width
}

// isFullWidthPunctuation 判断是否为全角 ASCII 标点（！ 到 ～）与 CJK 标点。
func isFullWidthPunctuation(r rune) bool {
	if r >= 0xFF01 && r <= 0xFF60 {
		return true
	}
	return unicode.Is(unicode.Punct, r) && r > 0x2000
}

// String 返回一行合计描述，例如「3 张，不含税 300.00，价税合计 339.00」。
func (s Summary) String() string {
	return summaryLine(s)
}

// summaryLine 返回一行合计描述。
func summaryLine(summary Summary) string {
	parts := []string{fmt.Sprintf("%d 张", summary.Count)}
	if summary.Amount != nil {
		parts = append(parts, "不含税 "+summary.Amount.Fixed(MoneyScale))
	}
	if summary.TaxAmount != nil {
		parts = append(parts, "税额 "+summary.TaxAmount.Fixed(MoneyScale))
	}
	if summary.TotalAmount != nil {
		parts = append(parts, "价税合计 "+summary.TotalAmount.Fixed(MoneyScale))
	}
	if summary.Red > 0 {
		parts = append(parts, fmt.Sprintf("红字 %d 张", summary.Red))
	}
	return strings.Join(parts, "，")
}
