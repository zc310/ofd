package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
)

// document_outlines.go 从 OFD 文档大纲提取可写入 PDF 的条目。
//
// PDF 大纲树在写出时是「按文档顺序的条目 + 嵌套层级」，而 OFD 大纲是嵌套树，
// 这里做一次深度优先展平。目标解析优先级与 pkg/webreader 的阅读器侧一致：
// 先取 Goto.Dest，其次按书签名在 Document.Bookmarks 中查找，最后才看 URI 动作。
//
// PDF 的每个大纲条目都必须有目标页，无法落到页面的条目会被丢弃——目录里列出标题
// 却点了跳到别处，比没有这条目录更糟。被丢弃的条目不留下层级空洞：其子项上移到
// 父项原有的层级上，树不会因为中间缺一层而散开。

// Outlines 返回文档大纲的扁平条目序列，按文档顺序排列。
//
// resolve 与 PageLinks 用的是同一套解析器：目标页必须在输出里才能建立跳转。
func (p *Document) Outlines(resolve PageLinkResolver) []drawing.PageOutline {
	if p == nil || p.Document == nil || p.Document.Outlines == nil {
		return nil
	}
	bookmarks := make(map[string]models.CtDest)
	if p.Document.Bookmarks != nil {
		for _, bookmark := range p.Document.Bookmarks.Bookmarks {
			if bookmark.Name != "" {
				bookmarks[bookmark.Name] = bookmark.Dest
			}
		}
	}
	outlines := make([]drawing.PageOutline, 0, len(p.Document.Outlines.OutlineElems))
	collectOutlines(p.Document.Outlines.OutlineElems, resolve, bookmarks, 0, &outlines)
	if len(outlines) == 0 {
		return nil
	}
	return outlines
}

// collectOutlines 递归展平大纲树。level 是已实际输出的祖先数量，因此被丢弃的条目
// 不会在层级里留下空洞。
func collectOutlines(elems []models.CTOutlineElem, resolve PageLinkResolver,
	bookmarks map[string]models.CtDest, level int, outlines *[]drawing.PageOutline) {
	for i := range elems {
		elem := &elems[i]
		// 子项层级按本项是否输出来定，不能直接改 level——那是所有兄弟项共用的，
		// 一旦在循环里自增，后续兄弟项会被逐层压成链而不是并列。
		childLevel := level
		if target, ok := outlineTarget(*elem, resolve, bookmarks); ok {
			*outlines = append(*outlines, drawing.PageOutline{
				Title:  elem.Title,
				Level:  level,
				Target: &target,
			})
			childLevel = level + 1
		}
		collectOutlines(elem.OutlineElem, resolve, bookmarks, childLevel, outlines)
	}
}

// outlineTarget 解析大纲项的页面目标。
func outlineTarget(elem models.CTOutlineElem, resolve PageLinkResolver,
	bookmarks map[string]models.CtDest) (drawing.LinkTarget, bool) {
	if elem.Actions == nil {
		return drawing.LinkTarget{}, false
	}
	for _, action := range elem.Actions.Actions {
		if action.Goto == nil {
			continue
		}
		if action.Goto.Dest != nil {
			return linkTarget(*action.Goto.Dest, resolve)
		}
		if action.Goto.Bookmark != nil {
			if dest, ok := bookmarks[action.Goto.Bookmark.Name]; ok {
				return linkTarget(dest, resolve)
			}
		}
	}
	return drawing.LinkTarget{}, false
}
