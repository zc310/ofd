package session

import (
	"context"

	"github.com/zc310/ofd/pkg/reader"
)

// AnnotationReport 是有界注解扫描的结果；Truncated 表示可能还有未扫描的注解。
type AnnotationReport struct {
	Items     []reader.AnnotationInfo `json:"items"`
	Scanned   int                     `json:"scanned"`
	Truncated bool                    `json:"truncated"`
}

// Annotations 扫描最多 2000 页、返回最多 5000 条注解；取消或出错时不返回部分结果。
func (s *Session) Annotations(ctx context.Context) (AnnotationReport, error) {
	return s.annotations(ctx, 2000, 5000)
}

func (s *Session) annotations(ctx context.Context, maxPages, maxItems int) (AnnotationReport, error) {
	return scan(ctx, s, func(ctx context.Context) (AnnotationReport, error) {
		pages, err := s.doc.Pages()
		if err != nil {
			return AnnotationReport{}, err
		}
		report := AnnotationReport{Items: []reader.AnnotationInfo{}}
		for index := 0; index < len(pages) && index < maxPages; index++ {
			if err := ctx.Err(); err != nil {
				return AnnotationReport{}, err
			}
			items, err := s.doc.AnnotationsForPage(index)
			if cancelErr := ctx.Err(); cancelErr != nil {
				return AnnotationReport{}, cancelErr
			}
			if err != nil {
				return AnnotationReport{}, err
			}
			report.Scanned++
			remaining := maxItems - len(report.Items)
			if len(items) > remaining {
				report.Items = append(report.Items, items[:remaining]...)
				report.Truncated = true
				return report, nil
			}
			report.Items = append(report.Items, items...)
			if len(report.Items) == maxItems && index+1 < len(pages) {
				report.Truncated = true
				return report, nil
			}
		}
		report.Truncated = report.Scanned < len(pages)
		return report, nil
	})
}

// FontUsage 统计指定字体的使用页；与搜索共享单个后台扫描名额。
func (s *Session) FontUsage(ctx context.Context, ref reader.FontRef, options reader.FontUsageOptions) (reader.FontUsage, error) {
	return scan(ctx, s, func(ctx context.Context) (reader.FontUsage, error) {
		return s.doc.FontUsage(ctx, ref, options)
	})
}

// FontUsageAll 批量统计字体使用页；会话关闭时取消扫描。
func (s *Session) FontUsageAll(ctx context.Context, options reader.FontUsageOptions) (reader.FontUsageReport, error) {
	return scan(ctx, s, func(ctx context.Context) (reader.FontUsageReport, error) {
		return s.doc.FontUsageAll(ctx, options)
	})
}

// Search 执行有界搜索；关闭会话时取消排队和运行中的搜索。
func (s *Session) Search(ctx context.Context, query string, options reader.SearchOptions) (reader.SearchReport, error) {
	return scan(ctx, s, func(ctx context.Context) (reader.SearchReport, error) {
		return s.doc.Search(ctx, query, options)
	})
}
