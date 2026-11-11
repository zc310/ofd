package contract

import "github.com/zc310/ofd/pkg/reader"

// Search 复制搜索结果并规范化空数组，不修改阅读器的快照。
func Search(report reader.SearchReport) reader.SearchReport {
	results := make([]reader.SearchResult, len(report.Results))
	for i, match := range report.Results {
		results[i] = match
		results[i].Rects = make([]reader.Rect, len(match.Rects))
		copy(results[i].Rects, match.Rects)
	}
	report.Results = results
	return report
}
