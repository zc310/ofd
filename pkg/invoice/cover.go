package invoice

import (
	"fmt"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

// 汇总页的排版常量，单位毫米。页面按 A4 竖排，版心左右各留 18mm。
const (
	coverPageWidth    = 210.0
	coverPageHeight   = 297.0
	coverMarginLeft   = 18.0
	coverMarginRight  = 18.0
	coverMarginTop    = 20.0
	coverMarginBottom = 20.0
	coverTitleSize    = 6.0
	coverTitleGap     = 3.0
	coverNoteSize     = 3.4
	coverNoteGap      = 3.0
	coverHeadSize     = 3.6
	coverBodySize     = 3.2
	// coverGap 是相邻两张表之间的纵向间距。
	coverGap = 7.0
	// coverTextHeightRatio 是文字框高相对字号的比例，文字基线落在框底。
	coverTextHeightRatio = 1.4
	// coverCaptionGap 是表题与表格内容之间的间距。
	coverCaptionGap = 2.5
	// coverCaptionBlock 是一行表题在表格内容上方占掉的高度：题底距表格顶边
	// 一个 coverCaptionGap，题身一个字号。创建器把表题画在 table.Y 之上，
	// 排布时必须把它单独算进这张表占用的高度，混进表格高度里等于让下一张
	// 表的表题正好落在上一张表的下边框上。
	coverCaptionBlock = coverCaptionGap + coverBodySize + 0.8

	// coverContentWidth 是版心宽度。
	coverContentWidth = coverPageWidth - coverMarginLeft - coverMarginRight
	// coverLabelWidth 是行首项目列的宽度，数值列从它之后开始。列宽合计等于版心
	// 宽度，多出的项目名才有地方放。
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
	// coverNote 是汇总页上固定出现的口径说明。
	coverNote = "金额单位：元；红字发票金额为负，已参与合计。"
)

// coverWidths 与 coverHeader 一一对应的列宽。
var coverWidths = []float64{coverLabelWidth, coverCountWidth, coverValueWidth, coverValueWidth, coverValueWidth}

// coverHeader 是各张表共用的表头。
var coverHeader = []string{"项目", "张数", "不含税金额", "税额", "价税合计"}

// 汇总页的纵向节奏。文字框高是字号的一点四倍，基线在框底，所以每行文字实际
// 占掉的高度要按框高算，漏算会让下一行与它重叠。
const (
	// coverTitleTop 是标题文字的顶边。
	coverTitleTop = coverMarginTop
	// coverNoteTop 是口径说明的顶边。
	coverNoteTop = coverTitleTop + coverTitleSize + coverTitleGap
	// coverFirstTableTop 是首页第一张表的顶边，已让出标题与口径说明。
	coverFirstTableTop = coverNoteTop + coverNoteSize*coverTextHeightRatio + coverNoteGap
	// coverContTableTop 是续页第一张表的顶边，续页没有标题与口径说明，但首张
	// 表通常带表题，表题画在顶边之上，要给它让出位置，否则会顶出版心上边。
	coverContTableTop = coverMarginTop + coverCaptionBlock
)

// coverPageProvider 按索引提供汇总页，是 creator.PageProvider 的实现。
type coverPageProvider struct {
	pages []creator.Page
}

func (p coverPageProvider) PageCount() int {
	return len(p.pages)
}

func (p coverPageProvider) PageAt(index int) (creator.Page, error) {
	if index < 0 || index >= len(p.pages) {
		return creator.Page{}, fmt.Errorf("汇总页页码 %d 超出范围（共 %d 页）", index+1, len(p.pages))
	}
	return p.pages[index], nil
}

// coverPages 生成汇总页。
//
// 每张表是独立的 creator.Table：换行、表头底色、框线与跨页切分都由创建器负责，
// 这里只决定"哪些表放哪一页"。放不下一页的表单独占一页，由创建器再切成多页。
func coverPages(summary Summary, title string) []creator.Page {
	if strings.TrimSpace(title) == "" {
		title = defaultCoverTitle
	}
	tables := coverTables(summary, title)

	var pages []creator.Page
	var placed []placedTable
	// 首页要留出标题与口径说明两行；续页没有它们，表格从版心顶边开始。
	y := coverFirstTableTop
	for _, table := range tables {
		// y 是这张表（含表题）的顶边。放不下就换页，单表比一页还高时也照样
		// 放上去，由创建器切分——这里不切是因为切了也不知道切在哪一行。
		if len(placed) > 0 && y+table.blockHeight() > coverPageHeight-coverMarginBottom {
			pages = append(pages, coverPage(len(pages) == 0, title, placed))
			placed = nil
			y = coverContTableTop
		}
		// placedTable.top 记的是表格内容的顶边：表题画在它上面 coverCaptionBlock 处。
		placed = append(placed, placedTable{table: table, top: y + table.captionHeight})
		y += table.blockHeight() + coverGap
	}
	if len(placed) > 0 {
		pages = append(pages, coverPage(len(pages) == 0, title, placed))
	}
	return pages
}

// placedTable 是一张表在某一页上的顶边位置。
type placedTable struct {
	table coverTable
	top   float64
}

// coverTable 是待排布的一张表，连同它占用的高度。
type coverTable struct {
	table creator.Table
	// height 是表格内容区的高度，不含表题。
	height float64
	// captionHeight 是表题占掉的高度，画在内容顶边之上；无表题为 0。
	captionHeight float64
}

// blockHeight 是这张表连表题一起占掉的高度，即它在纵向节奏里的步长。
func (c coverTable) blockHeight() float64 {
	return c.captionHeight + c.height
}

// coverPage 拼出一页汇总内容。标题与口径说明只在首页出现——续页是同一份汇总的
// 后续部分，再画一遍会与表格内容叠在一起。
func coverPage(first bool, title string, tables []placedTable) creator.Page {
	page := creator.Page{
		Area: &creator.PageArea{PhysicalBox: &creator.Box{
			X: 0, Y: 0, Width: coverPageWidth, Height: coverPageHeight,
		}},
	}
	if !first {
		for _, placed := range tables {
			table := placed.table.table
			table.Y = placed.top
			page.Items = append(page.Items, table)
		}
		return page
	}
	page.Items = append(page.Items, coverText(coverMarginLeft, coverTitleTop, coverContentWidth, coverTitleSize, title, true))
	page.Items = append(page.Items, coverText(coverMarginLeft, coverNoteTop, coverContentWidth, coverNoteSize, coverNote, false))

	for _, placed := range tables {
		table := placed.table.table
		table.Y = placed.top
		page.Items = append(page.Items, table)
	}
	return page
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
		Height: size * coverTextHeightRatio,
		Value:  value,
		Size:   size,
		Font:   coverFont,
		Weight: weight,
	}
}

// coverTables 把汇总结果拆成若干张表。
//
// 合计单独一张且不写表题：它是其余各表的基数，再标一次"合计"反而像另一组数据。
// 其中红字发票那行只有项目列有文字，其余列留空——空单元格不生成图元。
func coverTables(summary Summary, title string) []coverTable {
	total := [][]string{{
		fmt.Sprintf("发票合计 %d 张", summary.Count),
		fmt.Sprintf("%d", summary.Count),
		moneyText(summary.Amount),
		moneyText(summary.TaxAmount),
		moneyText(summary.TotalAmount),
	}}
	if summary.Red > 0 {
		total = append(total, []string{fmt.Sprintf("其中红字发票 %d 张", summary.Red), "", "", "", ""})
	}

	tables := []coverTable{coverTableOf("", total)}
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
			// 项目列写「名称（税号）」；税号是分组键，对账时要看的是它。
			// 列宽放不下时由创建器自动换行，不必在这里截断。
			label := entry.Name
			if entry.Key != "" && entry.Key != entry.Name {
				label = fmt.Sprintf("%s（%s）", entry.Name, entry.Key)
			}
			rows = append(rows, []string{
				label,
				fmt.Sprintf("%d", entry.Count),
				moneyText(entry.Amount),
				moneyText(entry.TaxAmount),
				moneyText(entry.TotalAmount),
			})
		}
		tables = append(tables, coverTableOf(group.title, rows))
	}
	return tables
}

// coverTableOf 造一张带表头与框线的表，并算出它占用的高度。
func coverTableOf(title string, rows [][]string) coverTable {
	table := creator.Table{
		X:            coverMarginLeft,
		Width:        coverContentWidth,
		ColumnWidths: coverWidths,
		HeaderRows:   1,
		Caption:      title,
		ContinuationCaption: func() string {
			if title == "" {
				return ""
			}
			return title + "（续）"
		}(),
		Style: creator.TableStyle{
			HorizontalLines: true,
			// 只画横线：竖线会把密密麻麻的数字框成一格格，表头底色已经够分区。
			VerticalLines:   false,
			LineWidth:       0.15,
			OuterLineWidth:  0.3,
			HeaderLineWidth: 0.25,
			HeaderFillColor: &creator.Color{R: 238, G: 238, B: 242},
			HeaderFont:      coverFont,
			HeaderSize:      coverHeadSize,
			HeaderWeight:    700,
		},
	}
	if title != "" {
		table.CaptionStyle = &creator.CaptionStyle{
			Font: coverFont, Size: coverBodySize + 0.8, Weight: 700,
			Align: "left", Gap: coverCaptionGap,
		}
	}

	cells := make([]creator.TableCell, 0, len(coverHeader))
	for index, name := range coverHeader {
		cells = append(cells, creator.TableCell{
			Value: name, Font: coverFont, Size: coverHeadSize, Weight: 700, Align: coverCellAlign(index),
		})
	}
	table.Rows = append(table.Rows, creator.TableRow{Cells: cells})

	for _, values := range rows {
		cells = make([]creator.TableCell, 0, len(values))
		for index, value := range values {
			if value == "" {
				// 空单元格不生成图元，红字发票那行的金额列就是占位空串。
				continue
			}
			cells = append(cells, creator.TableCell{
				Value: value, Font: coverFont, Size: coverBodySize, Align: coverCellAlign(index),
			})
		}
		table.Rows = append(table.Rows, creator.TableRow{Cells: cells})
	}

	// 高度只算内容区：表题画在内容顶边之上，由 captionHeight 单独记账。
	height, err := creator.TableHeight(table, coverPageWidth)
	if err != nil {
		// 高度算不出来（列数与行数不一致等）时按行数估一个，交给创建器报错，
		// 不在这里再叠一层校验错误。
		height = coverHeaderHeight() + float64(len(table.Rows))*coverRowHeight()
	}
	captionHeight := 0.0
	if title != "" {
		captionHeight = coverCaptionBlock
	}
	return coverTable{table: table, height: height, captionHeight: captionHeight}
}

// coverCellAlign 返回列的对齐方式：项目列居左，张数与三个金额列居右，
// 金额的小数点才能对齐。
func coverCellAlign(index int) string {
	if index == 0 {
		return "left"
	}
	return "right"
}

func coverHeaderHeight() float64 {
	return coverHeadSize*1.5 + 3.0
}

func coverRowHeight() float64 {
	return coverBodySize*1.5 + 3.0
}

func (s Summary) String() string {
	return summaryLine(s)
}

func summaryLine(summary Summary) string {
	parts := []string{fmt.Sprintf("%d 张", summary.Count)}
	if summary.Red > 0 {
		parts = append(parts, fmt.Sprintf("红字 %d 张", summary.Red))
	}
	parts = append(parts, fmt.Sprintf("价税合计 %s", moneyText(summary.TotalAmount)))
	return strings.Join(parts, " ")
}
