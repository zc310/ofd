package converter

import (
	"errors"
	"fmt"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

// ErrInvalidPage 表示请求的全局页码无效。
var ErrInvalidPage = errors.New("页码无效")

// documentPage 表示多文档体合并后的一个全局页面。
type documentPage struct {
	document   *render.Document
	page       *parser.Page
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
				page:       page,
				pageIndex:  pageIndex,
				pageNumber: len(pages) + 1,
			})
		}
	}
	return pages
}

func countDocumentPages(documents []*render.Document) int {
	total := 0
	for _, document := range documents {
		if document == nil || document.Document == nil {
			continue
		}
		for _, page := range document.Pages {
			if page != nil {
				total++
			}
		}
	}
	return total
}

func walkDocumentPages(documents []*render.Document, start, end int, fn func(documentPage) error) error {
	pageNumber := 0
	for _, document := range documents {
		if document == nil || document.Document == nil {
			continue
		}
		for pageIndex, page := range document.Pages {
			if page == nil {
				continue
			}
			if pageNumber >= start && pageNumber < end {
				if err := fn(documentPage{
					document:   document,
					page:       page,
					pageIndex:  pageIndex,
					pageNumber: pageNumber + 1,
				}); err != nil {
					return err
				}
			}
			pageNumber++
			if pageNumber >= end {
				return nil
			}
		}
	}
	return nil
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

// collectSelectedPages 按全局页序取出 [start, end) 区间的页面，供需要提前知道
// 输出页范围的调用方（如跳转目标换算）使用。
func collectSelectedPages(documents []*render.Document, start, end int) []documentPage {
	selected := make([]documentPage, 0, max(0, end-start))
	_ = walkDocumentPages(documents, start, end, func(page documentPage) error {
		selected = append(selected, page)
		return nil
	})
	return selected
}

// newPageLinkResolvers 按实际写出的页面，为每个文档体建立页 ID 到输出页序的解析器。
//
// 只收录 selected 中的页：被页码范围裁掉的页不会出现在输出里，指向它们的链接
// 无法解析，提取时会被丢弃。
func newPageLinkResolvers(selected []documentPage) map[*render.Document]render.PageLinkResolver {
	if len(selected) == 0 {
		return nil
	}
	byDocument := make(map[*render.Document]map[models.StID]render.LinkTargetRef)
	order := make([]*render.Document, 0, len(selected))
	for index, page := range selected {
		if page.document == nil || page.page == nil {
			continue
		}
		targets, ok := byDocument[page.document]
		if !ok {
			targets = make(map[models.StID]render.LinkTargetRef)
			byDocument[page.document] = targets
			order = append(order, page.document)
		}
		if _, exists := targets[page.page.ID]; exists {
			continue
		}
		targets[page.page.ID] = render.LinkTargetRef{
			Index:  index,
			Height: pageLinkTargetHeight(page),
		}
	}
	resolvers := make(map[*render.Document]render.PageLinkResolver, len(order))
	for _, document := range order {
		targets := byDocument[document]
		resolvers[document] = func(pageID models.StID) (render.LinkTargetRef, bool) {
			ref, ok := targets[pageID]
			return ref, ok
		}
	}
	return resolvers
}

// pageLinkTargetHeight 读取页面物理高度，用于把 OFD 的左上角坐标换算成 PDF 的
// 左下角坐标。只读页面 Area，不加载页面内容或资源。
//
// 页面自身没有定义有效尺寸时回退到文档公共 PageArea，再回退 A4——少了这一步，
// 跳转坐标会以 0 参与换算，`Dest@Type="XYZ" Left="0" Top="0"` 这类落点会被误判成
// 整页适配。
func pageLinkTargetHeight(page documentPage) float64 {
	box, err := page.page.PhysicalBoxMetadata()
	if err == nil && box.Width > 0 && box.Height > 0 {
		return box.Height
	}
	area := models.CtPageArea{}
	if page.document != nil && page.document.Document != nil {
		area = page.document.CommonData.PageArea
	}
	area.EnsurePhysicalBox()
	return area.PhysicalBox.Height
}
