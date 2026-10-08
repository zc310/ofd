package converter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/zc310/ofd/internal/render"
)

func init() { Register(&pdfEncoder{}) }

type pdfEncoder struct{}

func (e *pdfEncoder) Name() string         { return "pdf" }
func (e *pdfEncoder) Kind() Kind           { return KindDocument }
func (e *pdfEncoder) Extensions() []string { return []string{".pdf"} }
func (e *pdfEncoder) MIME() string         { return "application/pdf" }
func (e *pdfEncoder) Encode(input any, output io.Writer, conv *Converter) error {
	return encodeOFD(input, output, conv, e.encodeDocuments)
}

// pdfRenderOptions 是 PDF 输出选项：压缩内容流、子集化 TrueType 字体。
// JPEG 和不透明 PNG 按原始字节嵌入；透明图使用无损编码，避免重新压成 JPEG。
func pdfRenderOptions() render.PDFOptions {
	return render.PDFOptions{Compress: true, SubsetFonts: true, LossyImages: false}
}

func (e *pdfEncoder) encodeDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置 PDF 输出参数")
	}
	return pdfDocumentsWithConverter(documents, output, conv)
}

const maxPDFRenderWorkers = 4

// PDF 解析 input 中的 OFD 文档并按全局页码写入一个 PDF。
func PDF(ctx context.Context, input any, output io.Writer, opts ...Option) error {
	return Encode(ctx, "pdf", input, output, opts...)
}

// PDFDocuments 将多个已解析的 OFD 文档体按全局页码写入同一个 PDF。
func PDFDocuments(ctx context.Context, documents []*render.Document, output io.Writer, opts ...Option) error {
	return EncodeDocuments(ctx, "pdf", documents, output, opts...)
}

func pdfDocumentsWithConverter(documents []*render.Document, output io.Writer, conv *Converter) error {
	if !conv.pdfParallel {
		return pdfDocumentsSerial(documents, output, conv)
	}
	workers := min(runtime.GOMAXPROCS(0), maxPDFRenderWorkers)
	return pdfDocumentsWithWorkersConv(documents, output, workers, conv)
}

func pdfDocumentsSerial(documents []*render.Document, output io.Writer, conv *Converter) error {
	pages := collectDocumentPages(documents)
	if len(pages) == 0 {
		return errors.New("文档没有页面")
	}
	pageStart, pageEnd, err := pageRange(len(pages), conv.page)
	if err != nil {
		return err
	}
	pages = pages[pageStart:pageEnd]

	pdfDoc, err := render.NewPDFDocument(output, pdfRenderOptions())
	if err != nil {
		return err
	}
	// 跳转目标必须换算成输出页序，因此映射在写页之前建好。
	resolvers := newPageLinkResolvers(pages)
	for _, page := range pages {
		if err := conv.checkCancelled(); err != nil {
			return err
		}
		surface, err := page.document.Page(page.page)
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}
		links := page.document.PageLinks(page.page, resolvers[page.document])
		if err := pdfDoc.AddPage(surface, links); err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}
	}
	return pdfDoc.Close()
}

func pdfDocumentsWithWorkersConv(documents []*render.Document, output io.Writer, workers int, conv *Converter) error {
	if output == nil {
		return errors.New("未设置 PDF 输出参数")
	}
	totalPages := countDocumentPages(documents)
	if totalPages == 0 {
		return errors.New("文档没有页面")
	}
	pageStart, pageEnd, err := pageRange(totalPages, conv.page)
	if err != nil {
		return err
	}

	pdfDoc, err := render.NewPDFDocument(output, pdfRenderOptions())
	if err != nil {
		return err
	}
	// 跳转目标必须换算成输出页序，因此映射在写页之前建好。
	resolvers := newPageLinkResolvers(collectSelectedPages(documents, pageStart, pageEnd))
	workers = max(1, min(workers, pageEnd-pageStart))
	type pageJob struct {
		page     documentPage
		surface  render.VectorSurface
		err      error
		finished *sync.WaitGroup
	}
	jobs := make(chan *pageJob, workers)
	var pool sync.WaitGroup
	pool.Add(workers)
	for range workers {
		go func() {
			defer pool.Done()
			for job := range jobs {
				// 已取消时只把 finished 标记掉，不做渲染：否则取消后还要
				// 继续跑完这一批的全部页面才停。
				if err := conv.checkCancelled(); err != nil {
					job.err = err
					job.finished.Done()
					continue
				}
				job.surface, job.err = job.page.document.Page(job.page.page)
				job.finished.Done()
			}
		}()
	}
	defer func() {
		close(jobs)
		pool.Wait()
	}()

	// 每批先并行生成全部页面画布，再按页序串行写入 PDF。这样下一批
	// 不会在上一批写入期间访问共享的字体状态。
	batch := make([]documentPage, 0, workers)
	flush := func() error {
		// 每批派发前查一次。并行渲染是最贵的一段，而一批只做完几页，
		// 不查的话取消要等到整批结束才生效。
		if err := conv.checkCancelled(); err != nil {
			return err
		}
		jobsInBatch := make([]*pageJob, len(batch))
		var finished sync.WaitGroup
		finished.Add(len(batch))
		for index, page := range batch {
			job := &pageJob{page: page, finished: &finished}
			jobsInBatch[index] = job
			jobs <- job
		}
		finished.Wait()
		for index, page := range batch {
			job := jobsInBatch[index]
			if job.err != nil {
				return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, job.err)
			}
			if job.surface == nil {
				return fmt.Errorf("处理第%d页失败: 页面画布为空", page.pageNumber)
			}
			links := page.document.PageLinks(page.page, resolvers[page.document])
			if err := pdfDoc.AddPage(job.surface, links); err != nil {
				return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
			}
		}
		batch = batch[:0]
		return nil
	}
	if err := walkDocumentPages(documents, pageStart, pageEnd, func(page documentPage) error {
		batch = append(batch, page)
		if len(batch) == workers {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	if len(batch) > 0 {
		if err := flush(); err != nil {
			return err
		}
	}
	return pdfDoc.Close()
}

// pdfDocumentsWithWorkers 是测试用的向后兼容包装。
func pdfDocumentsWithWorkers(ctx context.Context, documents []*render.Document, output io.Writer, workers int) error {
	return pdfDocumentsWithWorkersConv(documents, output, workers, newConverter(ctx))
}
