package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

const thumbnailMaxPixels = 192

type thumbnailRequest struct {
	filename   string
	generation uint64
	page       int
	session    *continuousSession
	close      bool
}

type thumbnailResult struct {
	generation uint64
	page       int
	image      *image.RGBA
	err        error
}

// 缩略图使用独立的解析器与渲染文档，不与正在重放的页面共享文档关闭时序。
type thumbnailDocument struct {
	ofd       *parser.OFD
	documents []*render.Document
	pages     []viewerPage
	filename  string
}

func openThumbnailDocument(filename string) (model *thumbnailDocument, err error) { //nolint:nonamedreturns // 恢复异常时需要返回错误并释放部分构建的文档。
	return openThumbnailInput(filename, filename)
}

func openThumbnailInput(filename string, input any) (model *thumbnailDocument, err error) { //nolint:nonamedreturns // 恢复异常时需要返回错误并释放部分构建的文档。
	defer func() {
		if recovered := recover(); recovered != nil {
			model.close()
			model, err = nil, pageOperationFailure(recovered)
		}
	}()
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if ofd != nil {
			_ = ofd.Close()
		}
		return nil, err
	}
	model = &thumbnailDocument{ofd: ofd, filename: filename}
	for _, document := range ofd.Documents {
		doc := render.NewDocument(color.Transparent, document, geom.DPI(36))
		model.documents = append(model.documents, doc)
		if err := applyViewerFallback(doc); err != nil {
			model.close()
			return nil, err
		}
		for _, page := range document.Pages {
			model.pages = append(model.pages, viewerPage{document: doc, page: page})
		}
	}
	return model, nil
}

func (m *thumbnailDocument) close() {
	if m == nil {
		return
	}
	for _, doc := range m.documents {
		doc.Close()
	}
	if m.ofd != nil {
		_ = m.ofd.Close()
	}
}

func (m *thumbnailDocument) render(page int) (img *image.RGBA, err error) { //nolint:nonamedreturns // 将同步渲染中的异常转为页面错误。
	defer func() {
		if recovered := recover(); recovered != nil {
			img, err = nil, pageOperationFailure(recovered)
		}
	}()
	if page < 0 || page >= len(m.pages) {
		return nil, fmt.Errorf("%w: %d", errPageRange, page+1)
	}
	p := m.pages[page]
	lease, err := p.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil || content.Area == nil {
		return nil, errInvalidPageSize
	}
	box := content.Area.PhysicalBox
	if box.Width <= 0 || box.Height <= 0 || math.IsNaN(box.Width) || math.IsNaN(box.Height) || math.IsInf(box.Width, 0) || math.IsInf(box.Height, 0) {
		return nil, errInvalidPageSize
	}
	// 使用实际租约尺寸限制像素，不能仅信任元数据；保留少量余量避免浮点取整超限。
	dpmm := min(36.0/25.4, (thumbnailMaxPixels-1)/box.Width, (thumbnailMaxPixels-1)/box.Height)
	if dpmm <= 0 || math.IsNaN(dpmm) || math.IsInf(dpmm, 0) {
		return nil, errInvalidPageSize
	}
	return p.document.RasterizePage(p.page, render.BackendCanvas, geom.DPMM(dpmm))
}

// worker 独占缩略图文档；每次只处理一页，退出或换文档时释放所有资源。
func runThumbnailWorker(ctx context.Context, requests <-chan thumbnailRequest, results chan<- thumbnailResult, wake func()) {
	var model *thumbnailDocument
	var generation uint64
	defer func() { model.close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-requests:
			if !ok {
				return
			}
			if req.close {
				if req.generation >= generation {
					model.close()
					model = nil
					generation = req.generation
				}
				continue
			}
			var err error
			if model == nil || generation != req.generation || model.filename != req.filename {
				model.close()
				switch {
				case req.session != nil && req.session.view != nil:
					lease := req.session.acquire()
					if lease == nil {
						err = errNoDocument
					} else {
						model, err = openThumbnailInput(req.filename, req.session.view) //nolint:contextcheck // 解析器打开接口无 context。
						lease.release()
					}
				case req.session != nil && req.session.input != nil:
					model, err = openThumbnailInput(req.filename, req.session.input) //nolint:contextcheck // 同步解析接口无 context。
				default:
					model, err = openThumbnailDocument(req.filename) //nolint:contextcheck // 同步解析接口无 context；取消时停止发布并释放文档。
				}
				generation = req.generation
			}
			var img *image.RGBA
			if err == nil {
				img, err = model.render(req.page)
			}
			select {
			case <-ctx.Done():
				return
			case results <- thumbnailResult{generation: req.generation, page: req.page, image: img, err: err}:
				wake()
			}
		}
	}
}
