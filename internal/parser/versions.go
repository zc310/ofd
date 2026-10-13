package parser

import "fmt"

// SelectVersion 打开指定文档体的版本，其余文档体恢复主入口。
// id 为空时恢复所有主入口文档。失败时保留当前文档；调用方应排除并发读取。
func (p *OFD) SelectVersion(scope int, id string) error {
	if p == nil || p.fileCache == nil {
		return fmt.Errorf("OFD 文档已关闭")
	}
	if scope < 0 || scope >= len(p.mainDocuments) {
		return fmt.Errorf("文档体索引超出范围: %d", scope)
	}
	documents := append([]*Document(nil), p.mainDocuments...)
	if id != "" {
		main := p.mainDocuments[scope]
		version, err := main.LoadVersion(id)
		if err != nil {
			return err
		}
		if version == nil || version.DocRoot == "" {
			return fmt.Errorf("版本 %q 不存在或没有 DocRoot", id)
		}
		// GB/T 33190—2016 §7.3、§19.2（表 71）：DocRoot 是版本入口，
		// 相对路径以版本描述文件为基准；文件清单包含该版本全部依赖。
		base := main.versionLocations[id].Dir()
		for _, file := range version.FileList.Files {
			location := file.Path.Resolve(base)
			if !p.fileCache.Has(location.String()) {
				return fmt.Errorf("版本 %q 缺少文件: %s", id, location)
			}
		}
		body := p.DocBodies[scope]
		body.DocRoot = version.DocRoot.Resolve(base)
		document := &Document{pageCacheSize: p.options.PageCacheCapacity, pageCacheBytes: p.options.PageCacheBytes}
		document.Init(p.fileCache, body.DocRoot)
		if err := document.parse(body); err != nil {
			document.clearCaches()
			return fmt.Errorf("加载版本 %q 失败: %w", id, err)
		}
		if len(document.Pages) == 0 {
			document.clearCaches()
			return fmt.Errorf("版本 %q 没有页面", id)
		}
		// 主入口签章不能按复用的页面 ID 叠到独立历史页面上。
		// 入口相同则仍使用原文档，保留它已解析的签名与资源。
		if body.DocRoot == p.DocBodies[scope].DocRoot.Resolve("/") {
			document.clearCaches()
			documents[scope] = main
		} else {
			documents[scope] = document
		}
	}
	for scope, document := range p.Documents {
		if document != p.mainDocuments[scope] {
			document.clearCaches()
		}
	}
	p.Documents = documents
	return nil
}
