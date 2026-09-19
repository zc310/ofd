package creator

import "fmt"

// PageProvider 按索引提供页面，用于在页数或页模型大于内存时流式创建 OFD。
//
// 创建过程会在 ID 预留/校验阶段和写出阶段多次访问同一索引，因此实现必须能够
// 重复返回相同的页面。实现可以从磁盘、数据库或生成器中按需构造页面。
type PageProvider interface {
	// PageCount 返回文档页面总数。
	PageCount() int
	// PageAt 返回从 0 开始的第 index 页。
	PageAt(index int) (Page, error)
}

// slicePages 把内存中的页面切片适配为 PageProvider。
type slicePages struct {
	pages []Page
}

func (s slicePages) PageCount() int { return len(s.pages) }

func (s slicePages) PageAt(index int) (Page, error) {
	if index < 0 || index >= len(s.pages) {
		return Page{}, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return s.pages[index], nil
}
