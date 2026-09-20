package converter

import (
	"errors"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/textdoc"
)

func init() {
	Register(&textEncoder{})
	Register(&markdownEncoder{})
}

type textEncoder struct{}

func (e *textEncoder) Name() string         { return "text" }
func (e *textEncoder) Kind() Kind           { return KindDocument }
func (e *textEncoder) Extensions() []string { return []string{".txt"} }
func (e *textEncoder) MIME() string         { return "text/plain" }
func (e *textEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.textFromDocuments)
}

func (e *textEncoder) textFromDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	pages, err := extractPageTexts(documents, conv.page)
	if err != nil {
		return err
	}
	return textDocuments(pages, output)
}

func textDocuments(pages []string, output io.Writer) error {
	if output == nil {
		return errors.New("未设置文本输出参数")
	}
	text := strings.Join(pages, "\n\n")
	if text != "" {
		text += "\n"
	}
	_, err := io.WriteString(output, text)
	return err
}

// Text 提取 input 中 OFD 文档的文字并写入 output。
// 不会保留字体、颜色和布局信息；不同文字对象按行输出，不同页面使用分页符分隔。
func Text(input any, output io.Writer, opts ...Option) error {
	return Encode("text", input, output, opts...)
}

// TextDocument 提取已解析 OFD 文档中的文字并写入 output。
func TextDocument(doc *parser.Document, output io.Writer, opts ...Option) error {
	return TextDocuments([]*parser.Document{doc}, output, opts...)
}

// TextDocuments 按全局页码提取多个已解析 OFD 文档体中的文字并写入 output。
func TextDocuments(documents []*parser.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置文本输出参数")
	}
	docs := make([]*render.Document, len(documents))
	for i, d := range documents {
		docs[i] = &render.Document{Document: d}
	}
	return textFromRenderDocuments(docs, output, newConverter(opts...))
}

func textFromRenderDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	pages, err := extractPageTexts(documents, conv.page)
	if err != nil {
		return err
	}
	return textDocuments(pages, output)
}

// extractPageTexts 提取每个页面的纯文本表示。
func extractPageTexts(documents []*render.Document, page int) ([]string, error) {
	pages, err := collectTextPages(documents, page)
	if err != nil {
		return nil, err
	}
	textPages := make([]string, len(pages))
	for i, p := range pages {
		textPages[i] = strings.Join(arrangeTextLayout(p.Entries, p.Width), "\n")
	}
	return textPages, nil
}

// collectTextPages 按全局页码校验并提取页面。
func collectTextPages(documents []*render.Document, page int) ([]textdoc.Page, error) {
	docs := parserDocuments(documents)
	total := textdoc.Count(docs)
	if total == 0 {
		return nil, errors.New("文档没有页面")
	}
	start, end, err := pageRange(total, page)
	if err != nil {
		return nil, err
	}
	return textdoc.Collect(docs, start, end), nil
}

// parserDocuments 从 render.Document 中提取底层 parser.Document。
func parserDocuments(documents []*render.Document) []*parser.Document {
	docs := make([]*parser.Document, len(documents))
	for i, d := range documents {
		if d != nil {
			docs[i] = d.Document
		}
	}
	return docs
}

const textLayoutColumns = 80

// arrangeTextLayout 将文字对象按 y 聚成行、行内按 x 排列，并用前导和间隔空格
// 近似还原列位置。纯文本只有行列两个维度，因此只能做到近似对齐。
func arrangeTextLayout(entries []textdoc.Entry, pageWidth float64) []string {
	valid := make([]textdoc.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Text == "" || !textdoc.Finite(entry.X) || !textdoc.Finite(entry.Y) {
			continue
		}
		valid = append(valid, entry)
	}
	if len(valid) == 0 {
		return nil
	}
	sort.SliceStable(valid, func(i, j int) bool {
		if valid[i].Y != valid[j].Y {
			return valid[i].Y < valid[j].Y
		}
		return valid[i].X < valid[j].X
	})

	unit := pageWidth / textLayoutColumns
	if !textdoc.Finite(unit) || unit <= 0 {
		unit = 1
	}
	minX := valid[0].X
	for _, entry := range valid[1:] {
		if entry.X < minX {
			minX = entry.X
		}
	}

	lines := make([]string, 0, len(valid))
	for start := 0; start < len(valid); {
		rowY := valid[start].Y
		rowSize := valid[start].Size
		end := start + 1
		for end < len(valid) {
			if valid[end].Y-rowY > textdoc.RowTolerance(rowSize, valid[end].Size, unit) {
				break
			}
			if valid[end].Size > rowSize {
				rowSize = valid[end].Size
			}
			end++
		}
		row := valid[start:end]
		sort.SliceStable(row, func(i, j int) bool { return row[i].X < row[j].X })
		lines = append(lines, layoutTextRow(row, minX, unit))
		start = end
	}
	return lines
}

func layoutTextRow(row []textdoc.Entry, minX, unit float64) string {
	var builder strings.Builder
	width := 0

	for index, entry := range row {
		column := int(math.Round((entry.X - minX) / unit))
		if column < 0 {
			column = 0
		}
		switch {
		case index == 0:
			builder.WriteString(strings.Repeat(" ", column))
		case column > width:
			builder.WriteString(strings.Repeat(" ", column-width))
		default:
			builder.WriteByte(' ')
			column = width + 1
		}
		builder.WriteString(entry.Text)
		width = column + textdoc.DisplayWidth(entry.Text)
	}
	return builder.String()
}
