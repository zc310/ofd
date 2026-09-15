package converter

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"runtime"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

const maxPDFRenderWorkers = 4

func PDF(input any, output io.Writer, opts ...Option) error {
	conv := newConverter(opts...)
	ofd, err := parser.NewOFDWithOptions(input, parser.Options{
		PageCacheCapacity: conv.pageCacheCapacity,
		PageCacheBytes:    conv.pageCacheBytes,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err = ofd.Close(); err != nil {
			slog.Error(err.Error())
		}
	}()
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档")
	}

	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocumentWithDPI(color.Transparent, document, conv.dpi))
	}
	return PDFDocuments(documents, output, opts...)
}

// PDFDocuments 将多个已解析的 OFD 文档体按全局页码写入同一个 PDF。
func PDFDocuments(documents []*render.Document, output io.Writer, opts ...Option) error {
	if output == nil {
		return errors.New("未设置 PDF 输出参数")
	}
	conv := newConverter(opts...)
	if !conv.pdfParallel {
		return pdfDocumentsSerial(documents, output, conv)
	}
	workers := min(runtime.GOMAXPROCS(0), maxPDFRenderWorkers)
	return pdfDocumentsWithWorkers(documents, output, workers, opts...)
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

func pdfDocumentsWithWorkers(documents []*render.Document, output io.Writer, workers int, opts ...Option) error {
	if output == nil {
		return errors.New("未设置 PDF 输出参数")
	}
	conv := newConverter(opts...)
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
