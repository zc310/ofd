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
			images[index] = textdoc.ExtractImages(page.Owner, page.Source, maxDocxImagesPerPage)
		}
	}
	return docx.Write(output, docxPagesOptions(pages, conv.docTitle), func(w *docx.Writer) error {
		for index, page := range pages {
			// 页循环内必须查取消。textdoc.Collect 一次性提取了整个页码区间、
			// 没有逐页钩子，所以取消只能补在这一层。
			if err := conv.checkCancelled(); err != nil {
				return err
			}
			if err := writeDocxPage(w, page, index, len(pages), conv.DOCXTables(), conv.DOCXAnnotations(), images[index]); err != nil {
				return fmt.Errorf("处理第%d页失败: %w", index+1, err)
			}
		}
		return nil
	})
}

// maxDocxImagesPerPage 限制单页内嵌的图片数量。整版图片类的 OFD（例如把每页
// 当成一张扫描图）会在这里被截断，避免长篇扫描件产出体量失控的 docx。
const maxDocxImagesPerPage = 64

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
func writeDocxPage(w *docx.Writer, page textdoc.Page, index, total int, detectTables, includeAnnotations bool, images []textdoc.Image) error {
	for _, block := range docxBlockSequence(page.Entries, page.Height, detectTables, images, includeAnnotations) {
		var err error
		switch block.kind {
		case docxBlockImage:
			err = w.AddImage(block.image.Data, block.image.Extension, block.image.Width, block.image.Height)
		case docxBlockTable:
			err = w.AddTable(block.docxTable())
		case docxBlockHeading, docxBlockParagraph:
			paragraph := block.docxParagraph()
			// 整行只有空白时 trim 后不剩内容，产出的是没有 w:r 的空段落。
			// 它对版面没有贡献，还会在 Word 里占一行，直接跳过。
			if len(paragraph.Runs) == 0 {
				continue
			}
			err = w.AddParagraph(paragraph)
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
// 输出是流式版面：OFD 的绝对坐标被转换成 Word 段落、标题与表格，文字可直接
// 编辑与重排，但换字体或改文字后版式会随之变化——这与 OFD 的固定版式不同。
// 字号与字体名按 OFD 原值写入，不嵌入字体文件，实际效果取决于打开方的系统字体。
// 图片、矢量图形与列表暂不输出，只保留文字与表格。
func DOCX(ctx context.Context, input any, output io.Writer, opts ...Option) error {
	return Encode(ctx, "docx", input, output, opts...)
}

// DOCXDocuments 将多个已解析的 OFD 文档体按全局页码写入同一个 DOCX 文件。
func DOCXDocuments(ctx context.Context, documents []*render.Document, output io.Writer, opts ...Option) error {
	return EncodeDocuments(ctx, "docx", documents, output, opts...)
}
