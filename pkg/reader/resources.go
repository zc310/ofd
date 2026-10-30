package reader

import (
	"fmt"
	"sort"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

type scopedFont struct {
	scope int
	id    models.StID
	font  *models.Font
}

type scopedMedia struct {
	scope    int
	id       models.StID
	media    *models.MultiMedia
	document *parser.Document
}

type resourceCatalog struct {
	fonts []scopedFont
	media []scopedMedia
}

// resourceCatalogLocked 返回文档生命周期内稳定的资源目录。
// 调用方必须持有 Reader.metadataMu。
func (r *Reader) resourceCatalogLocked() (resourceCatalog, error) {
	r.metadata.resourcesOnce.Do(func() {
		r.metadata.resourcesVal, r.metadata.resourcesErr = r.computeResourceCatalog()
	})
	return r.metadata.resourcesVal, r.metadata.resourcesErr
}

func (r *Reader) computeResourceCatalog() (resourceCatalog, error) {
	var catalog resourceCatalog
	if r.ofd == nil {
		return catalog, nil
	}
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		fonts, media, err := document.DeclaredResources()
		if err != nil {
			return resourceCatalog{}, fmt.Errorf("读取文档体 %d 的资源声明失败: %w", scope, err)
		}
		for id, font := range fonts {
			if font != nil {
				catalog.fonts = append(catalog.fonts, scopedFont{scope: scope, id: id, font: font})
			}
		}
		for id, item := range media {
			if item != nil {
				catalog.media = append(catalog.media, scopedMedia{scope: scope, id: id, media: item, document: document})
			}
		}
	}
	sort.Slice(catalog.fonts, func(i, j int) bool {
		if catalog.fonts[i].scope != catalog.fonts[j].scope {
			return catalog.fonts[i].scope < catalog.fonts[j].scope
		}
		return catalog.fonts[i].id < catalog.fonts[j].id
	})
	sort.Slice(catalog.media, func(i, j int) bool {
		if catalog.media[i].scope != catalog.media[j].scope {
			return catalog.media[i].scope < catalog.media[j].scope
		}
		return catalog.media[i].id < catalog.media[j].id
	})
	return catalog, nil
}
