package invoice

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 汇总页的长度使用毫米；传给 flow 的字号统一换算为磅。
const (
	coverDefaultPageWidth  = 210.0
	coverDefaultPageHeight = 297.0
	coverMarginLeft        = 18.0
	coverMarginRight       = 18.0
	coverMarginTop         = 20.0
	coverMarginBottom      = 20.0
	coverTitleSize         = 6.0
	coverTitleGap          = 3.0
	coverNoteSize          = 3.4
	coverNoteGap           = 3.0
	coverHeadSize          = 3.6
	coverBodySize          = 3.2
	coverGap               = 7.0
	coverCaptionGap        = 2.5
	coverLabelRatio        = 0.39
	coverCountRatio        = 0.09
	creatorName            = "zc310/ofd ofd-invoice"
	coverFont              = "楷体"
	defaultCoverTitle      = "发票汇总对账表"
	coverNote              = "金额单位：元；红字发票金额为负，已参与合计。"
)

func coverHeader() []string {
	return []string{"项目", "张数", "不含税金额", "税额", "价税合计"}
}

// coverGeometry 根据实际输出纸张计算列宽，汇总页始终纵向。
type coverGeometry struct {
	labelWidth, countWidth, valueWidth float64
}

func newCoverGeometry(pageWidth float64) coverGeometry {
	content := pageWidth - coverMarginLeft - coverMarginRight
	label, count := content*coverLabelRatio, content*coverCountRatio
	return coverGeometry{labelWidth: label, countWidth: count, valueWidth: (content - label - count) / 3}
}

func (g coverGeometry) widths() []float64 {
	return []float64{g.labelWidth, g.countWidth, g.valueWidth, g.valueWidth, g.valueWidth}
}

// coverPageProvider 提供已经由 flow 完成分页的汇总页。
type coverPageProvider struct {
	pages []creator.Page
}

func (p coverPageProvider) PageCount() int { return len(p.pages) }

func (p coverPageProvider) PageAt(index int) (creator.Page, error) {
	if index < 0 || index >= len(p.pages) {
		return creator.Page{}, fmt.Errorf("汇总页页码 %d 超出范围（共 %d 页）", index+1, len(p.pages))
	}
	return p.pages[index], nil
}

// coverModel 统一由 flow 测量、换行、分页；不再自行估算表高或分配表格页码。
func coverModel(ctx context.Context, summary Summary, title string, pageWidth, pageHeight float64) (*creator.Document, error) {
	if strings.TrimSpace(title) == "" {
		title = defaultCoverTitle
	}
	opts := flow.DefaultOptions()
	opts.PageSize = creator.PageSize{Width: pageWidth, Height: pageHeight}
	opts.MarginLeft, opts.MarginRight = coverMarginLeft, coverMarginRight
	opts.MarginTop, opts.MarginBottom = coverMarginTop, coverMarginBottom
	opts.AllowLogicalFonts = true
	opts.Fonts = []creator.Font{{Name: coverFont, FontName: coverFont, FamilyName: coverFont, Charset: "prc"}}
	opts.BoldFont, opts.ItalicFont, opts.BoldItalicFont = "", "", ""
	opts.Theme.Body = flow.TextStyle{Font: coverFont, Size: coverNoteSize * 72 / 25.4, LineHeight: 1.4, SpaceAfter: coverNoteGap}
	opts.Theme.Table = flow.TextStyle{Font: coverFont, Size: coverBodySize * 72 / 25.4, LineHeight: 1.5, SpaceAfter: coverGap}
	opts.Theme.Caption = flow.TextStyle{Font: coverFont, Size: (coverBodySize + 0.8) * 72 / 25.4, LineHeight: 1.4, SpaceAfter: coverCaptionGap, Align: "left"}
	doc := flow.New(opts)
	doc.Metadata.ID, doc.Metadata.Title, doc.Metadata.Creator = "invoice-summary", title, creatorName
	doc.Blocks = append(doc.Blocks,
		flow.Block{Runs: []flow.Run{{Text: title, Bold: true}}, Style: &flow.TextStyle{Size: coverTitleSize * 72 / 25.4, SpaceAfter: coverTitleGap}},
		flow.Block{Text: coverNote})
	for _, table := range coverTables(summary, newCoverGeometry(pageWidth)) {
		doc.AddTable(table)
	}
	return doc.Build(ctx)
}

// coverTables 只决定汇总内容，布局与跨页重复表头交给 flow。
func coverTables(summary Summary, geometry coverGeometry) []flow.Table {
	total := [][]string{{
		fmt.Sprintf("发票合计 %d 张", summary.Count), strconv.Itoa(summary.Count),
		moneyText(summary.Amount), moneyText(summary.TaxAmount), moneyText(summary.TotalAmount),
	}}
	if summary.Red > 0 {
		total = append(total, []string{fmt.Sprintf("其中红字发票 %d 张", summary.Red), "", "", "", ""})
	}
	tables := []flow.Table{coverTableOf("", total, geometry)}
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
			label := entry.Name
			if entry.Key != "" && entry.Key != entry.Name {
				label = fmt.Sprintf("%s（%s）", entry.Name, entry.Key)
			}
			rows = append(rows, []string{label, strconv.Itoa(entry.Count),
				moneyText(entry.Amount), moneyText(entry.TaxAmount), moneyText(entry.TotalAmount)})
		}
		tables = append(tables, coverTableOf(group.title, rows, geometry))
	}
	return tables
}

func coverTableOf(title string, rows [][]string, geometry coverGeometry) flow.Table {
	hidden := &flow.TableBorder{}
	table := flow.Table{HeaderRows: 1, Caption: title, RepeatCaption: title != "", ContinuationSuffix: "（续）",
		CellStyle: &flow.CellStyle{VerticalAlign: "middle", Borders: &flow.CellBorders{
			Left: hidden, Right: hidden, Top: &flow.TableBorder{Width: 0.075}, Bottom: &flow.TableBorder{Width: 0.075},
		}}}
	for index, width := range geometry.widths() {
		align := "right"
		if index == 0 {
			align = "left"
		}
		table.Columns = append(table.Columns, flow.TableColumn{Width: width, Align: align})
	}
	header := make([]flow.TableCell, len(coverHeader()))
	for index, name := range coverHeader() {
		header[index] = flow.TableCell{Text: name, Style: &flow.TextStyle{Size: coverHeadSize * 72 / 25.4},
			Appearance: &flow.CellStyle{Borders: &flow.CellBorders{
				Top: &flow.TableBorder{Width: 0.3}, Bottom: &flow.TableBorder{Width: 0.25},
			}}}
	}
	table.Grid = append(table.Grid, header)
	for _, values := range rows {
		cells := make([]flow.TableCell, len(values))
		for index, value := range values {
			// 空格位仍保留网格，但不生成空文字图元。
			cells[index] = flow.TableCell{Text: value}
		}
		table.Grid = append(table.Grid, cells)
	}
	return table
}

func (s Summary) String() string { return summaryLine(s) }

func summaryLine(summary Summary) string {
	parts := []string{fmt.Sprintf("%d 张", summary.Count)}
	if summary.Red > 0 {
		parts = append(parts, fmt.Sprintf("红字 %d 张", summary.Red))
	}
	parts = append(parts, fmt.Sprintf("价税合计 %s", moneyText(summary.TotalAmount)))
	return strings.Join(parts, " ")
}
