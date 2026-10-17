package webreader

import (
	"errors"

	"github.com/zc310/ofd/internal/render"
)

// SelectVersion 按版本 DocRoot 重建指定文档体的页面和资源，其余文档体恢复主入口。
// scope 与 ID 来自 Versions；ID 为空时恢复所有主入口文档。失败时保留当前视图。
// 切换成功后，调用方必须重新读取页数、页面尺寸、字体和导航信息。
func (r *Reader) SelectVersion(scope int, id string) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	if err := r.ofd.SelectVersion(scope, id); err != nil {
		return err
	}
	r.closeRenderDocumentsLocked()
	for documentIndex, document := range r.ofd.Documents {
		renderDocument := render.NewDocument(r.options.Background, document)
		for _, family := range r.fallbackFamilies {
			_ = renderDocument.UseFallbackFont(family)
		}
		for _, page := range renderDocument.Pages {
			r.pages = append(r.pages, pageRef{document: renderDocument, page: page, fontScope: documentIndex})
		}
	}
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	r.metadata = &readerMetadata{}
	r.hiddenLayers = nil
	return nil
}
