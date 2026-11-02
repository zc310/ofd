package parser

import (
	"fmt"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

// LoadTemplateResources 同时取得模板内容及其只读资源视图。
// §7.7 表12允许模板使用 PageRes；§7.9的资源路径基于资源描述文件解析。
// 视图只继承公共/文档资源，不借用调用页面的专属资源；没有 PageRes 时 scope 为 nil。
// 调用方必须持有原文档的页面租约；视图借用 FileCache，不得关闭它。
func (p *Document) LoadTemplateResources(id models.StID) (*models.PageContent, *Document, error) {
	p.templateMu.Lock()
	defer p.templateMu.Unlock()
	content, err := p.loadTemplateLocked(id)
	if err != nil || content == nil {
		return nil, nil, err
	}
	scope := p.templateResources[id]
	// 超大模板可能不进入 LRU；返回时仍须保留与本次内容对应的资源。
	if scope == nil && len(content.PageRes) != 0 {
		scope, _, err = p.loadTemplateResources(content, p.templateLocations[id])
	}
	return content, scope, err
}

func (p *Document) loadTemplateResources(content *models.PageContent, location spec.StLoc) (*Document, int64, error) {
	if len(content.PageRes) == 0 {
		return nil, 0, nil
	}
	// 不复制包含锁的 Document；每个视图有独立索引和同步状态。
	p.resourcesMu.RLock()
	scope := &Document{Document: p.Document, BaseLoc: p.BaseLoc, FileCache: p.FileCache,
		res: cloneResourceMap(p.baseRes), fontRes: cloneResourceMap(p.baseFontRes),
		drawParams: cloneResourceMap(p.baseDrawParams), colorSpaces: cloneResourceMap(p.baseColorSpaces),
		compositeUnits: cloneResourceMap(p.baseCompositeUnits)}
	p.resourcesMu.RUnlock()
	var weight int64
	seen := make(map[string]bool)
	for _, ref := range content.PageRes {
		resourcePath := p.resolvePageResourcePath(location.Dir(), ref)
		data, err := p.FileCache.Read(resourcePath.String())
		if err != nil {
			return nil, 0, fmt.Errorf("读取模板 %s 资源 %s 失败: %w", location, resourcePath, err)
		}
		resource, err := models.ParseResXML(data)
		if err != nil {
			return nil, 0, fmt.Errorf("读取模板 %s 资源 %s 失败: %w", location, resourcePath, err)
		}
		scope.collectResource(resource, resourcePath, true, nil)
		if !seen[resourcePath.String()] {
			seen[resourcePath.String()] = true
			if entry, ok := p.FileCache.Lookup(resourcePath.String()); ok {
				weight += int64(min(entry.UncompressedSize, uint64((1<<63-1)-weight)))
			}
		}
	}
	return scope, weight, nil
}
