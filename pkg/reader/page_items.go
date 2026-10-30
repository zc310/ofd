package reader

import "github.com/zc310/ofd/internal/models"

// walkPageItems 按文档顺序迭代展开 PageBlock，避免输入嵌套消耗 Go 调用栈。
// visit 返回 false 时停止，不访问后续图元。
func walkPageItems(items []models.PageItem, visit func(models.PageItem) bool) {
	var stack [][]models.PageItem
	for {
		if len(items) == 0 {
			if len(stack) == 0 {
				return
			}
			items = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			continue
		}
		item := items[0]
		items = items[1:]
		if item.Kind == models.PageItemBlock {
			if item.Block != nil && len(item.Block.Items) != 0 {
				stack = append(stack, items)
				items = item.Block.Items
			}
			continue
		}
		if !visit(item) {
			return
		}
	}
}
