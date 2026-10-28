package render

import (
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/navigation"
)

// Outlines 返回文档大纲的扁平条目序列，目标页解析与 PageLinks 共用同一契约。
func (p *Document) Outlines(resolve PageLinkResolver) []drawing.PageOutline {
	if p == nil || p.Document == nil {
		return nil
	}
	return navigation.Outlines(p.Document.Outlines, p.Bookmarks, resolve)
}
