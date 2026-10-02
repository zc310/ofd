package converter

import (
	"context"
	"errors"
	"io"
	"strings"

	markdownpkg "github.com/zc310/ofd/internal/markdown"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

type markdownEncoder struct{}

func (e *markdownEncoder) Name() string         { return "markdown" }
func (e *markdownEncoder) Kind() Kind           { return KindDocument }
func (e *markdownEncoder) Extensions() []string { return []string{".md", ".markdown"} }
func (e *markdownEncoder) MIME() string         { return "text/markdown" }
func (e *markdownEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.markdownFromDocuments)
}

func (e *markdownEncoder) markdownFromDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	return markdownDocuments(documents, output, conv)
}

// Markdown 提取 input 中 OFD 文档的文字并写入 Markdown 文档。
// “第 X 章”识别为二级标题，其他标题按字号和编号层级输出。
func Markdown(ctx context.Context, input any, output io.Writer, opts ...Option) error {
	return Encode(ctx, "markdown", input, output, opts...)
}

// MarkdownDocument 提取已解析 OFD 文档中的文字并写入 Markdown 文档。
func MarkdownDocument(ctx context.Context, doc *parser.Document, output io.Writer, opts ...Option) error {
	return MarkdownDocuments(ctx, []*parser.Document{doc}, output, opts...)
}

// MarkdownDocuments 按全局页码提取多个已解析 OFD 文档体中的文字并写入 Markdown 文档。
func MarkdownDocuments(ctx context.Context, documents []*parser.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置Markdown输出参数")
	}
	docs := make([]*render.Document, len(documents))
	for i, d := range documents {
		docs[i] = &render.Document{Document: d}
	}
	return markdownDocuments(docs, output, newConverter(ctx, opts...))
}

func markdownDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置Markdown输出参数")
	}
	if err := conv.checkCancelled(); err != nil {
		return err
	}
	pages, err := collectTextPages(documents, conv.page)
	if err != nil {
		return err
	}
	// textdoc.Collect 一次性提取区间内所有页，没有逐页钩子，因此只能在
	// 前后各查一次。文字提取比栅格化轻得多，这个粒度够用。
	if err := conv.checkCancelled(); err != nil {
		return err
	}

	var markdown strings.Builder
	title := strings.TrimSpace(conv.docTitle)
	if title == "" {
		title = "OFD 文档"
	}
	markdown.WriteString("# ")
	markdown.WriteString(markdownpkg.EscapeLine(title))
	markdown.WriteString("\n\n")

	firstPage := true
	for _, page := range pages {
		if len(page.Entries) == 0 {
			continue
		}
		if !firstPage {
			markdown.WriteString("\n---\n\n")
		}
		firstPage = false
		markdownpkg.RenderPage(&markdown, page.Entries, page.Height, conv.MarkdownTables())
	}

	_, err = io.WriteString(output, markdown.String())
	return err
}
