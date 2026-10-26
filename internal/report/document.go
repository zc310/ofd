// Package report 提供报告格式推断、flow 排版、字体加载与标准文档输出。
package report

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

const Width = 174.0

var (
	Ink    = creator.Color{R: 35, G: 48, B: 62}
	Muted  = creator.Color{R: 91, G: 104, B: 117}
	Green  = creator.Color{R: 38, G: 125, B: 85}
	Red    = creator.Color{R: 184, G: 59, B: 58}
	Orange = creator.Color{R: 186, G: 113, B: 25}
	Blue   = creator.Color{R: 47, G: 104, B: 157}
)

// Builder 累积报告内容，Build 统一返回文字准备或分页错误。
type Builder struct {
	Document *flow.Document
	ctx      context.Context
	fonts    map[string]Font
	err      error
}

// New 基于 flow 默认 Material 主题创建 A4、18mm 页边距、中文页码和章节大纲的报告。
func New(ctx context.Context, metadata creator.Document, fontPath string) (*Builder, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := LoadFont(fontPath)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	opts := flow.DefaultOptions()
	opts.MarginTop, opts.MarginRight, opts.MarginBottom, opts.MarginLeft = 18, 18, 18, 18
	opts.Fonts = []creator.Font{{Name: "Flow-Regular", Data: f.Data}}
	opts.BoldFont, opts.ItalicFont, opts.BoldItalicFont = "", "", ""
	fonts := map[string]Font{"Flow-Regular": f}
	if bold, ok := loadBoldFont(f); ok {
		opts.Fonts = append(opts.Fonts, creator.Font{Name: "Flow-Bold", Data: bold.Data, Bold: true})
		opts.BoldFont = "Flow-Bold"
		fonts["Flow-Bold"] = bold
	} else {
		// 没有同族粗体时，沿用实际常规字体；主题的字号、间距和配色保持默认。
		for i := range opts.Theme.Headings {
			opts.Theme.Headings[i].Font = "Flow-Regular"
		}
	}
	opts.GenerateOutlines = true
	opts.Footer = "第 {page} 页 / 共 {pages} 页"
	doc := flow.New(opts)
	doc.Metadata = metadata
	return &Builder{Document: doc, ctx: ctx, fonts: fonts}, nil
}

// Paragraph 添加可跨页的正文段落，color 为空时使用正文颜色。
func (b *Builder) Paragraph(text string, color *creator.Color) {
	style := b.Document.Options.Theme.Body
	if color != nil {
		style.Color = color
	}
	b.Block("paragraph", 0, text, style)
}

// Heading 添加标题及其大纲，color 为空时使用标题颜色。
func (b *Builder) Heading(level int, text string, color *creator.Color) {
	if level < 1 || level > 6 {
		b.err = fmt.Errorf("报告标题级别必须为 1 到 6")
		return
	}
	style := b.Document.Options.Theme.Headings[level-1]
	if color != nil {
		style.Color = color
	}
	b.Block("heading", level, text, style)
}

// Block 按块级样式添加内容，提前处理长路径和不可显示字符。
func (b *Builder) Block(kind string, level int, text string, style flow.TextStyle) {
	if b.err != nil {
		return
	}
	font, ok := b.fonts[style.Font]
	if !ok {
		b.err = fmt.Errorf("报告字体 %q 未注册", style.Font)
		return
	}
	prepared, err := font.PrepareText(b.ctx, text, style.Size, Width)
	if err != nil {
		b.err = err
		return
	}
	b.Document.Blocks = append(b.Document.Blocks, flow.Block{Kind: kind, Level: level, Text: prepared, Style: &style})
}

// TableOptions 配置报告表格的跨页方式。
type TableOptions struct {
	// SplitRows 允许长单元格按完整文字行跨页续排，续页仍重复表头。
	SplitRows bool
	// ColumnAlign 按列设置表头和正文的水平对齐，空字符串沿用主题。
	ColumnAlign []string
}

// Table 添加带重复表头的网格表格；列宽包含单元格四周 1.5mm 的内边距。
func (b *Builder) Table(headers []string, rows [][]flow.TableCell, widths []float64, options ...TableOptions) {
	if b.err != nil {
		return
	}
	if len(headers) == 0 || len(headers) != len(widths) {
		b.err = fmt.Errorf("报告表格列宽与表头数量不一致")
		return
	}
	table := flow.Table{HeaderRows: 1, ColumnWidths: widths}
	if len(options) > 0 {
		table.SplitRows = options[0].SplitRows
	}
	header := make([]flow.TableCell, len(headers))
	for i, text := range headers {
		header[i].Text = text
	}
	all := append([][]flow.TableCell{header}, rows...)
	for r, row := range all {
		if len(row) != len(headers) {
			b.err = fmt.Errorf("报告表格行与表头列数不一致")
			return
		}
		cells := append([]flow.TableCell(nil), row...)
		for i := range cells {
			if len(options) > 0 && i < len(options[0].ColumnAlign) && options[0].ColumnAlign[i] != "" {
				style := flow.TextStyle{}
				if cells[i].Style != nil {
					style = *cells[i].Style
				}
				style.Align = options[0].ColumnAlign[i]
				cells[i].Style = &style
			}
			size := b.Document.Options.Theme.Table.Size
			name := b.Document.Options.Theme.Table.Font
			if cells[i].Style != nil && cells[i].Style.Size > 0 {
				size = cells[i].Style.Size
			}
			if cells[i].Style != nil && cells[i].Style.Font != "" {
				name = cells[i].Style.Font
			}
			if r == 0 && name == "Flow-Regular" && b.Document.Options.BoldFont != "" {
				name = b.Document.Options.BoldFont
			}
			font, ok := b.fonts[name]
			if !ok {
				b.err = fmt.Errorf("报告字体 %q 未注册", name)
				return
			}
			prepared, err := font.PrepareText(b.ctx, cells[i].Text, size, widths[i]-3)
			if err != nil {
				b.err = err
				return
			}
			cells[i].Text = prepared
		}
		table.Grid = append(table.Grid, cells)
	}
	b.Document.AddTable(table)
}

// Build 返回完成分页的标准 creator 模型。
func (b *Builder) Build() (*creator.Document, error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.Document.Build(b.ctx)
}

// WriteOFD 将已分页模型写出，按实际使用字形子集化嵌入字体。
func WriteOFD(ctx context.Context, w io.Writer, document *creator.Document) error {
	if ctx == nil || w == nil || document == nil {
		return fmt.Errorf("报告上下文、输出和文档不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := creator.CreateWithOptions(*document, contextio.Writer(ctx, w), creator.CreateOptions{Deterministic: true})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// WritePDF 先在内存中生成 OFD，再转换为可搜索的矢量 PDF。
func WritePDF(ctx context.Context, w io.Writer, document *creator.Document) error {
	if w == nil {
		return fmt.Errorf("PDF 输出写入器为空")
	}
	var ofd bytes.Buffer
	if err := WriteOFD(ctx, &ofd, document); err != nil {
		return err
	}
	return converter.PDF(ctx, ofd.Bytes(), w)
}
