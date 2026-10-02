package converter

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/zc310/ofd/internal/docx"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/textdoc"
)

// 本文件只负责把 DOCX 编码器接进转换注册表，并把文档流交给 internal/docx。
// 版面结构推断（标题层级、段落边界、表格、列表）在 internal/docx 的 blocks.go
// 与 lists.go 里，它们复用 internal/textdoc 的 Entry/Rows/DetectTables，
// 不依赖本包，因此 DOCX 的实现细节不散落在各个编码器之间。

func init() { Register(&docxEncoder{}) }

type docxEncoder struct{}

func (e *docxEncoder) Name() string         { return "docx" }
func (e *docxEncoder) Kind() Kind           { return KindDocument }
func (e *docxEncoder) Extensions() []string { return []string{".docx"} }
func (e *docxEncoder) MIME() string {
	return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
}

func (e *docxEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.encodeDocuments)
}

func (e *docxEncoder) encodeDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置 DOCX 输出参数")
	}
	pages, err := collectTextPages(documents, conv.page)
	if err != nil {
		return err
	}
	images := map[int][]textdoc.Image{}
	if conv.DOCXImages() {
		for index, page := range pages {
			images[index] = textdoc.ExtractImages(page.Owner, page.Source, docx.MaxImagesPerPage)
		}
	}
	return docx.Write(output, docxPagesOptions(pages, conv.docTitle), func(w *docx.Writer) error {
		for index, page := range pages {
			// 页循环内必须查取消。textdoc.Collect 一次性提取了整个页码区间、
			// 没有逐页钩子，所以取消只能补在这一层。
			if err := conv.checkCancelled(); err != nil {
				return err
			}
			if err := writeDocxPage(w, page, index, len(pages), docx.PageOptions{
				Tables:      conv.DOCXTables(),
				Annotations: conv.DOCXAnnotations(),
			}, images[index]); err != nil {
				return fmt.Errorf("处理第%d页失败: %w", index+1, err)
			}
		}
		return nil
	})
}

// docxPagesOptions 以首个有内容的页面尺寸作为整篇文档的纸张大小。OFD 允许每页
// 尺寸不同，而 DOCX 的页面设置是节级属性，流式输出不做逐页分节，统一按首页
// 版式排版，其余页若尺寸不同会被 Word 按同一纸张缩放。
func docxPagesOptions(pages []textdoc.Page, title string) docx.Options {
	options := docx.Options{Title: title}
	for _, page := range pages {
		if textdoc.Finite(page.Width) && textdoc.Finite(page.Height) &&
			page.Width > 0 && page.Height > 0 {
			options.PageWidth = docx.Twips(page.Width)
			options.PageHeight = docx.Twips(page.Height)
			break
		}
	}
	return options
}

// writeDocxPage 输出一页的块序列，并在需要时补分页符。
func writeDocxPage(w *docx.Writer, page textdoc.Page, index, total int, options docx.PageOptions, images []textdoc.Image) error {
	for _, block := range docx.BlockSequence(page.Entries, page.Height, options, images) {
		var err error
		switch block.Kind {
		case docx.BlockImage:
			err = w.AddImage(block.Image.Data, block.Image.Extension, block.Image.Width, block.Image.Height)
		case docx.BlockTable:
			err = w.AddTable(block.Table())
		default:
			err = w.AddParagraph(block.Paragraph())
		}
		if err != nil {
			return err
		}
	}
	// 页与页之间补分页符；最后一页不补，否则末尾会多出一个空段落。
	if index+1 < total {
		return w.AddPageBreak()
	}
	return nil
}

// DOCX 将 input 中的 OFD 文档转换为单个 WordprocessingML 文档。
//
// 输出是流式版面：OFD 的绝对坐标被转换成 Word 段落、标题、列表、表格与内嵌
// 图片，文字可直接编辑与重排，但换字体或改文字后版式会随之变化——这与 OFD 的
// 固定版式不同。字号与字体名按 OFD 原值写入，不嵌入字体文件，实际效果取决于打开
// 方的系统字体。批注层的水印与印章默认剔除，可用 WithDOCXAnnotations 保留。
func DOCX(ctx context.Context, input any, output io.Writer, opts ...Option) error {
	return Encode(ctx, "docx", input, output, opts...)
}

// DOCXDocuments 将多个已解析的 OFD 文档体按全局页码写入同一个 DOCX 文件。
func DOCXDocuments(ctx context.Context, documents []*render.Document, output io.Writer, opts ...Option) error {
	return EncodeDocuments(ctx, "docx", documents, output, opts...)
}
