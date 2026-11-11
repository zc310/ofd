package session

import (
	"context"

	"github.com/zc310/ofd/pkg/reader"
)

// AnnotationsForPage 使用有界的逐页元数据名额。取消发生在读取期间时丢弃结果。
func (s *Session) AnnotationsForPage(ctx context.Context, index int) ([]reader.AnnotationInfo, error) {
	return limited(ctx, s, s.pageMetadata, func(_ context.Context) ([]reader.AnnotationInfo, error) {
		items, err := s.doc.AnnotationsForPage(index)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []reader.AnnotationInfo{}
		}
		return items, nil
	})
}

// PageLinksForPage 按需读取一页链接，关闭或取消时不返回结果。
func (s *Session) PageLinksForPage(ctx context.Context, index int) ([]reader.PageLink, error) {
	return limited(ctx, s, s.pageMetadata, func(_ context.Context) ([]reader.PageLink, error) {
		return s.doc.PageLinksForPage(index)
	})
}

// Layers 按需读取单页图层，取消或关闭时丢弃结果。
func (s *Session) Layers(ctx context.Context, index int) ([]reader.LayerInfo, error) {
	return limited(ctx, s, s.pageMetadata, func(_ context.Context) ([]reader.LayerInfo, error) {
		layers, err := s.doc.Layers(index)
		if err != nil {
			return nil, err
		}
		result := make([]reader.LayerInfo, len(layers))
		copy(result, layers)
		return result, nil
	})
}

// SetHiddenLayers 原子更新页面显示状态；关闭会等待已经开始的更新完成。
func (s *Session) SetHiddenLayers(index int, ids []string) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.active.Done()
	return s.doc.SetHiddenLayers(index, ids)
}
