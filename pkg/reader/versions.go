package reader

import (
	"errors"
	"fmt"

	"github.com/zc310/ofd/internal/parser"
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
	newPages := make([]pageRef, 0, len(r.pages))
	newDocuments := make([]*render.Document, 0, len(r.ofd.Documents))
	closeNewDocuments := func() {
		for _, document := range newDocuments {
			document.Close()
		}
	}
	err := r.ofd.SelectVersionPrepared(scope, id, func(documents []*parser.Document) error {
		for documentIndex, document := range documents {
			if len(document.Pages) == 0 {
				continue
			}
			renderDocument := render.NewDocument(r.options.Background, document)
			newDocuments = append(newDocuments, renderDocument)
			for _, family := range r.fallbackFamilies {
				if err := renderDocument.UseFallbackFont(family); err != nil {
					return fmt.Errorf("切换版本后应用回退字体 %q 失败: %w", family, err)
				}
			}
			for _, page := range renderDocument.Pages {
				newPages = append(newPages, pageRef{document: renderDocument, page: page, fontScope: documentIndex})
			}
		}
		return nil
	})
	if err != nil {
		closeNewDocuments()
		return err
	}
	// 新文档先取得回退字体引用，再释放旧文档；延迟注销的字体不会在两者之间被删除。
	r.closeRenderDocumentsLocked()
	r.pages = newPages
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	r.metadata = &readerMetadata{}
	r.hiddenLayers = nil
	return nil
}
