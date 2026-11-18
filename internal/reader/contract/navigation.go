package contract

import (
	"maps"

	"github.com/zc310/ofd/pkg/reader"
)

// Versions 保持浏览器与桌面共用的版本字段和空数组语义。
func Versions(infos []reader.VersionInfo) []map[string]any {
	result := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		pages := append([]int{}, info.Pages...)
		result = append(result, map[string]any{"scope": info.Scope, "id": info.ID, "index": info.Index,
			"current": info.Current, "version": info.Version, "name": info.Name,
			"creation_date": info.CreationDate, "pages": pages, "doc_root": info.DocRoot})
	}
	return result
}

// CustomTags 将只读标签快照映射为共用界面的字段，不把 XML 当作 HTML。
func CustomTags(trees []reader.CustomTagTree) []map[string]any {
	result := make([]map[string]any, 0, len(trees))
	for _, tree := range trees {
		item := map[string]any{"namespace": tree.Namespace, "path": tree.Path, "root": customTagNode(tree.Root)}
		if tree.TypeID != "" {
			item["type_id"] = tree.TypeID
		}
		result = append(result, item)
	}
	return result
}

func customTagNode(node *reader.CustomTagNode) any {
	if node == nil {
		return nil
	}
	children := make([]any, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, customTagNode(child))
	}
	attributes := make(map[string]string, len(node.Attributes))
	maps.Copy(attributes, node.Attributes)
	var target any
	if t := node.Target; t != nil {
		target = map[string]any{"page": t.Page, "page_id": t.PageID, "object_id": t.ObjectID,
			"x": t.X, "y": t.Y, "width": t.Width, "height": t.Height, "object_text": t.ObjectText}
	}
	return map[string]any{"name": node.Name, "namespace": node.Namespace, "attributes": attributes,
		"text": node.Text, "children": children, "target": target}
}
