package main

import (
	"strings"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/navigation"
	"github.com/zc310/ofd/pkg/spec"
)

const maxNavigationDepth = 64

// 导航模型由后台构建，发布后只读；不向界面暴露解析器和标准模型的引用。
type documentNavigation struct {
	outlines  []navigationItem
	bookmarks []navigationItem
}

type navigationItem struct {
	title    string
	depth    int
	end      int // 子树结束位置，不包含该位置。
	expanded bool
	link     pageLink
}

func (m *documentModel) pageResolver(document *render.Document) render.PageLinkResolver {
	return func(id spec.StID) (render.LinkTargetRef, bool) {
		index, ok := m.pageIndexes[document][id]
		if !ok {
			return render.LinkTargetRef{}, false
		}
		metadata := readPageMetadata(m.pages[index])
		return render.LinkTargetRef{Index: index, Height: metadata.size.height}, metadata.err == nil
	}
}

func (m *documentModel) readNavigation() *documentNavigation {
	model := new(documentNavigation)
	for _, document := range m.documents {
		resolve := m.pageResolver(document)
		bookmarks := make(map[string]spec.Dest)
		if document.Bookmarks != nil {
			for _, bookmark := range document.Bookmarks.Bookmarks {
				bookmarks[bookmark.Name] = bookmark.Dest
				model.bookmarks = append(model.bookmarks, navigationItem{
					title: navigationTitle(bookmark.Name, "未命名书签"),
					link:  navigationGoto(&spec.ActionGoto{Dest: &bookmark.Dest}, resolve, nil),
				})
			}
		}
		if document.Document.Outlines != nil {
			appendNavigationOutlines(&model.outlines, document.Document.Outlines.OutlineElems, 0, resolve, bookmarks)
		}
	}
	return model
}

// GB/T 33190—2016 §7.8 表17：纯标题节点也保留，Expanded 省略时默认展开。
// 深度上限只用于资源保护，不作为标准限制；与渲染导航的64层访问上限一致。
func appendNavigationOutlines(items *[]navigationItem, elems []spec.OutlineElem, depth int, resolve render.PageLinkResolver, bookmarks map[string]spec.Dest) {
	if depth >= maxNavigationDepth {
		return
	}
	for _, elem := range elems {
		index := len(*items)
		item := navigationItem{
			title: navigationTitle(elem.Title, "未命名目录项"), depth: depth,
			expanded: elem.Expanded == nil || *elem.Expanded,
		}
		if elem.Actions != nil {
			for _, action := range elem.Actions.Actions {
				link := navigationGoto(action.Goto, resolve, bookmarks)
				if link.target != nil {
					item.link = link
					break
				}
				if item.link.uri == "" && action.URI != nil {
					item.link.uri, _ = webURL(navigation.URI(action.URI))
				}
			}
		}
		*items = append(*items, item)
		appendNavigationOutlines(items, elem.OutlineElem, depth+1, resolve, bookmarks)
		(*items)[index].end = len(*items)
	}
}

// 大纲激活和书签选择不是页面 CLICK 事件；只借用统一解析器解析其 Goto 目标。
func navigationGoto(gotoAction *spec.ActionGoto, resolve render.PageLinkResolver, bookmarks map[string]spec.Dest) pageLink {
	if gotoAction == nil {
		return pageLink{}
	}
	actions := spec.Actions{Action: []spec.Action{{Event: spec.ActionEventClick, Goto: gotoAction}}}
	link, ok := navigation.ActionLinkWithBookmarks(actions, resolve, bookmarks)
	if !ok || !validLinkTarget(link.Target) {
		return pageLink{}
	}
	return pageLink{target: link.Target}
}

func navigationTitle(title, fallback string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	return fallback
}
