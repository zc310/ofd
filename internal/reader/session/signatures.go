package session

import (
	"context"

	"github.com/zc310/ofd/pkg/reader"
)

// SignatureDetails 返回解析器已有的签名验证明细；未执行的检查保留未完成状态。
func (s *Session) SignatureDetails(ctx context.Context) ([]reader.SignatureInfo, error) {
	return limited(ctx, s, s.searches, func(_ context.Context) ([]reader.SignatureInfo, error) {
		return s.doc.Signatures()
	})
}

// SignatureSummary 仅包含签名声明，不代表任何验签结论。
type SignatureSummary struct {
	Scope    int                     `json:"scope"`
	ID       string                  `json:"id"`
	Provider string                  `json:"provider"`
	Company  string                  `json:"company"`
	Version  string                  `json:"version"`
	Method   string                  `json:"method"`
	Date     string                  `json:"date"`
	Stamps   []SignatureStampSummary `json:"stamps"`
}

// SignatureStampSummary 是签章声明的位置，不包含印章数据或验签结果。
type SignatureStampSummary struct {
	ID              string                     `json:"id"`
	Page            int                        `json:"page"`
	Boundary        *reader.AnnotationBoundary `json:"boundary"`
	VisibleBoundary *reader.AnnotationBoundary `json:"visible_boundary"`
}

// SignatureSummaries 返回签名声明和位置，不执行或输出验签结果。
func (s *Session) SignatureSummaries(ctx context.Context) ([]SignatureSummary, error) {
	return limited(ctx, s, s.searches, func(ctx context.Context) ([]SignatureSummary, error) {
		infos, err := s.doc.Signatures()
		if err != nil {
			return nil, err
		}
		result := make([]SignatureSummary, 0, len(infos))
		for _, info := range infos {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			summary := SignatureSummary{Scope: info.Scope, ID: info.ID, Provider: info.Provider, Company: info.Company, Version: info.Version, Method: info.Method, Date: info.Date, Stamps: make([]SignatureStampSummary, 0, len(info.Stamps))}
			for _, stamp := range info.Stamps {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				var boundary, visible *reader.AnnotationBoundary
				if stamp.Boundary != nil {
					value := *stamp.Boundary
					boundary = &value
				}
				if stamp.VisibleBoundary != nil {
					value := *stamp.VisibleBoundary
					visible = &value
				}
				summary.Stamps = append(summary.Stamps, SignatureStampSummary{ID: stamp.ID, Page: stamp.Page, Boundary: boundary, VisibleBoundary: visible})
			}
			result = append(result, summary)
		}
		return result, nil
	})
}
