package main

import (
	"context"
	"fmt"
)

// 元数据错误的页面保留 A4 占位空间及错误，不触发页面渲染。
type continuousPageMetadata struct {
	size continuousPageSize
	err  error
}

func readContinuousMetadata(ctx context.Context, model *documentModel) ([]continuousPageMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errNoDocument
	}
	metadata := make([]continuousPageMetadata, len(model.pages))
	for i, page := range model.pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metadata[i] = readPageMetadata(page)
	}
	return metadata, nil
}

func readPageMetadata(page viewerPage) (metadata continuousPageMetadata) { //nolint:nonamedreturns // 隔离单页元数据异常，保留占位页及错误。
	metadata.size = continuousPageSize{width: 210, height: 297}
	defer func() {
		if recovered := recover(); recovered != nil {
			metadata.err = fmt.Errorf("%w: %v", errPageOperation, recovered)
		}
	}()
	box, err := page.page.EffectivePhysicalBoxMetadata()
	switch {
	case err != nil:
		metadata.err = err
	case !validPositive(box.Width) || !validPositive(box.Height):
		metadata.err = errInvalidPageSize
	default:
		metadata.size = continuousPageSize{width: box.Width, height: box.Height}
	}
	return metadata
}

// visiblePageBatch 仅调度与视口相交且未就绪、未失败的页面。
// ready 包含已缓存与在途页面；调用方负责丢弃已过期批次并限制持有的页面。
func visiblePageBatch(g continuousGeometry, metadata []continuousPageMetadata, top, bottom float64, ready func(int) bool) []int {
	first, end := g.visible(top, bottom)
	end = min(end, len(metadata))
	pages := make([]int, 0, min(pageSceneCacheEntries, max(0, end-first)))
	for page := first; page < end && len(pages) < pageSceneCacheEntries; page++ {
		if metadata[page].err == nil && !ready(page) {
			pages = append(pages, page)
		}
	}
	return pages
}
