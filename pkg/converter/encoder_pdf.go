package converter

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"
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

func (e *pdfEncoder) encodeDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	if output == nil {
		return errors.New("未设置 PDF 输出参数")
	}
	return pdfDocumentsWithConverter(documents, output, conv)
}

const maxPDFRenderWorkers = 4

// PDF 解析 input 中的 OFD 文档并按全局页码写入一个 PDF。
func PDF(input any, output io.Writer, opts ...Option) error {
	return Encode("pdf", input, output, opts...)
}

// PDFDocuments 将多个已解析的 OFD 文档体按全局页码写入同一个 PDF。
func PDFDocuments(documents []*render.Document, output io.Writer, opts ...Option) error {
	return EncodeDocuments("pdf", documents, output, opts...)
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

	var pdfDoc *pdf.PDF
	for _, page := range pages {
		c, err := page.document.Page(page.document.Pages[page.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}
		if pdfDoc == nil {
			pdfDoc = pdf.New(output, c.W, c.H, nil)
		} else {
			pdfDoc.NewPage(c.W, c.H)
		}
		c.RenderTo(pdfDoc)
	}
	if pdfDoc == nil {
		return errors.New("PDF 文档创建失败")
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

	var pdfDoc *pdf.PDF
	workers = max(1, min(workers, pageEnd-pageStart))
	type pageJob struct {
		page     documentPage
		canvas   *canvas.Canvas
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
				job.canvas, job.err = job.page.document.Page(job.page.document.Pages[job.page.pageIndex])
				job.finished.Done()
			}
		}()
	}
	defer func() {
		close(jobs)
		pool.Wait()
	}()

	// 每批先并行生成全部页面画布，再按页序串行写入 PDF。这样下一批
	// 不会在上一批 RenderTo 期间访问共享的字体状态。
	batch := make([]documentPage, 0, workers)
	flush := func() error {
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
			if job.canvas == nil {
				return fmt.Errorf("处理第%d页失败: 页面画布为空", page.pageNumber)
			}
			if pdfDoc == nil {
				pdfDoc = pdf.New(output, job.canvas.W, job.canvas.H, nil)
			} else {
				pdfDoc.NewPage(job.canvas.W, job.canvas.H)
			}
			job.canvas.RenderTo(pdfDoc)
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
	if pdfDoc == nil {
		return errors.New("PDF 文档创建失败")
	}
	return pdfDoc.Close()
}

// pdfDocumentsWithWorkers 是测试用的向后兼容包装。
func pdfDocumentsWithWorkers(documents []*render.Document, output io.Writer, workers int) error {
	return pdfDocumentsWithWorkersConv(documents, output, workers, newConverter())
}
