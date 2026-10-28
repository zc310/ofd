package navigation

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
)

// 从 OFD 文档大纲提取可写入 PDF 的条目。
//
// PDF 大纲树在写出时是「按文档顺序的条目 + 嵌套层级」，而 OFD 大纲是嵌套树，
// 这里做一次深度优先展平。目标解析优先级与 pkg/webreader 的阅读器侧一致：
// 先取 Goto.Dest，其次按书签名在 Document.Bookmarks 中查找，最后才看 URI 动作。
//
// 有有效页面目标或 URI 的条目均保留。无法解释的条目不留下层级空洞，子项上移。

// Outlines 返回文档大纲的扁平条目序列，按文档顺序排列。
//
// resolve 与 ActionLink 共用页面映射：目标页必须在输出里才能建立跳转。
func Outlines(source *models.OutlineList, bookmarkList *models.BookmarkList, resolve PageResolver) []drawing.PageOutline {
	if source == nil {
		return nil
	}
	bookmarks := make(map[string]models.CtDest)
	if bookmarkList != nil {
		for _, bookmark := range bookmarkList.Bookmarks {
			if bookmark.Name != "" {
				bookmarks[bookmark.Name] = bookmark.Dest
			}
		}
	}
	outlines := make([]drawing.PageOutline, 0, len(source.OutlineElems))
	collectOutlines(source.OutlineElems, resolve, bookmarks, 0, &outlines)
	if len(outlines) == 0 {
		return nil
	}
	return outlines
}

// collectOutlines 递归展平大纲树。level 是已实际输出的祖先数量，因此被丢弃的条目
// 不会在层级里留下空洞。
func collectOutlines(elems []models.CTOutlineElem, resolve PageResolver,
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
		} else if uri := outlineURI(*elem); uri != "" {
			*outlines = append(*outlines, drawing.PageOutline{Title: elem.Title, Level: level, URI: uri})
			childLevel = level + 1
		}
		collectOutlines(elem.OutlineElem, resolve, bookmarks, childLevel, outlines)
	}
}

func outlineURI(elem models.CTOutlineElem) string {
	if elem.Actions != nil {
		for _, action := range elem.Actions.Actions {
			if action.URI != nil && action.URI.URI != "" {
				return action.URI.URI
			}
		}
	}
	return ""
}

// outlineTarget 解析大纲项的页面目标。
func outlineTarget(elem models.CTOutlineElem, resolve PageResolver,
	bookmarks map[string]models.CtDest) (drawing.LinkTarget, bool) {
	if elem.Actions == nil {
		return drawing.LinkTarget{}, false
	}
	for _, action := range elem.Actions.Actions {
		if action.Goto == nil {
			continue
		}
		if action.Goto.Dest != nil {
			if target, ok := linkTarget(*action.Goto.Dest, resolve); ok {
				return target, true
			}
		}
		if action.Goto.Bookmark != nil {
			if dest, ok := bookmarks[action.Goto.Bookmark.Name]; ok {
				if target, ok := linkTarget(dest, resolve); ok {
					return target, true
				}
			}
		}
	}
	return drawing.LinkTarget{}, false
}
