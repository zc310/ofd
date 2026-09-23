package parser

import (
	"fmt"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

// readPageContentXML 读取并解析页面内容 XML 的快速路径。错误包装保持与
// core.Package.ReadXMLLimit 一致的形状，避免调用方感知差异。
func readPageContentXML(fc *core.Package, path string, limit int64) (*models.PageContent, error) {
	data, err := fc.ReadLimit(path, limit)
	if err != nil {
		return nil, fmt.Errorf("读取 XML 文件失败: %w", err)
	}
	content, err := models.ParsePageContentXML(data)
	if err != nil {
		return nil, err
	}
	return content, nil
}

// xmlReadLimit 与 internal/core 中 xmlReadLimit 相同的限流语义。
func xmlReadLimit(size uint64) int64 {
	const extra = uint64(1024)
	const maxInt64 = uint64(1<<63 - 1)
	if size > maxInt64-extra {
		return int64(maxInt64)
	}
	return int64(size + extra)
}
