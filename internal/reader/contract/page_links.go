package contract

import "github.com/zc310/ofd/pkg/reader"

// PageLink 保留网页对无效边界和空组合动作的表示。
type PageLink struct {
	reader.PageLink

	Boundary *reader.AnnotationBoundary `json:"boundary"`
	Actions  []PageLink                 `json:"actions,omitempty"`
}

// PageLinks 复制链接树，规范化空列表与无效边界。
func PageLinks(links []reader.PageLink) []PageLink {
	result := make([]PageLink, len(links))
	for i, link := range links {
		result[i].PageLink = link
		if link.Boundary.Width > 0 && link.Boundary.Height > 0 {
			boundary := link.Boundary
			result[i].Boundary = &boundary
		}
		if len(link.Actions) > 0 {
			result[i].Actions = PageLinks(link.Actions)
		}
	}
	return result
}
