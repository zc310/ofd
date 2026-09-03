package converter

import (
	"errors"
	"fmt"

	"github.com/zc310/ofd/internal/render"
)

// ErrInvalidPage 表示请求的全局页码无效。
var ErrInvalidPage = errors.New("页码无效")

// documentPage 表示多文档体合并后的一个全局页面。
type documentPage struct {
	document   *render.Document
	pageIndex  int
	pageNumber int
}

func collectDocumentPages(documents []*render.Document) []documentPage {
	pages := make([]documentPage, 0)
	for _, document := range documents {
		if document == nil || document.Document == nil {
			continue
		}
		for pageIndex, page := range document.Pages {
			if page == nil {
				continue
			}
			pages = append(pages, documentPage{
				document:   document,
				pageIndex:  pageIndex,
				pageNumber: len(pages) + 1,
			})
		}
	}
	return pages
}

func pageRange(total, page int) (int, int, error) {
	if page < 0 {
		return 0, 0, fmt.Errorf("%w: %d（页码不能小于 0）", ErrInvalidPage, page)
	}
	if page > total {
		return 0, 0, fmt.Errorf("%w: %d（共 %d 页）", ErrInvalidPage, page, total)
	}
	if page == 0 {
		return 0, total, nil
	}
	return page - 1, page, nil
}
