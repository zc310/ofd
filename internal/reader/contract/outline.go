package contract

import "github.com/zc310/ofd/pkg/reader"

func outlineNodes(nodes []reader.OutlineNode) []reader.OutlineNode {
	result := make([]reader.OutlineNode, len(nodes))
	for i, node := range nodes {
		result[i] = node
		result[i].Children = outlineNodes(node.Children)
	}
	return result
}

// Outline 复制大纲列表并规范化空数组，不修改阅读器的快照。
func Outline(tree reader.OutlineTree) reader.OutlineTree {
	tree.Nodes = outlineNodes(tree.Nodes)
	bookmarks := make([]reader.Bookmark, len(tree.Bookmarks))
	copy(bookmarks, tree.Bookmarks)
	tree.Bookmarks = bookmarks
	return tree
}
