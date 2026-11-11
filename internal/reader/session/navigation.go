package session

import (
	"context"
	"errors"

	"github.com/zc310/ofd/pkg/reader"
)

type navigationDocument interface {
	CustomTags() ([]reader.CustomTagTree, error)
	Versions() ([]reader.VersionInfo, error)
	SelectVersion(scope int, id string) error
}

var errNavigationUnavailable = errors.New("文档不支持导航扩展")

// CustomTags 按有界后台名额读取标签；取消后不发布迟到结果。
func (s *Session) CustomTags(ctx context.Context) ([]reader.CustomTagTree, error) {
	return limited(ctx, s, s.searches, func(_ context.Context) ([]reader.CustomTagTree, error) {
		doc, ok := s.doc.(navigationDocument)
		if !ok {
			return nil, errNavigationUnavailable
		}
		return doc.CustomTags()
	})
}

// Versions 返回当前会话可用的历史版本。
func (s *Session) Versions(ctx context.Context) ([]reader.VersionInfo, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) ([]reader.VersionInfo, error) {
		doc, ok := s.doc.(navigationDocument)
		if !ok {
			return nil, errNavigationUnavailable
		}
		return doc.Versions()
	})
}

// CustomTags 返回指定会话的标签快照，不进行前端字段转换。
func (s *Service) CustomTags(ctx context.Context, prefix string) ([]reader.CustomTagTree, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.CustomTags(ctx)
}

// Versions 返回指定会话的版本快照，不进行前端字段转换。
func (s *Service) Versions(ctx context.Context, prefix string) ([]reader.VersionInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.Versions(ctx)
}

// SelectVersion 先独立准备目标版本；失败保留当前会话，成功才撤销旧地址。
// 发布属于不可撤销的状态写入，调用方应等待真实结果，不能仅取消前端 Promise。
func (s *Service) SelectVersion(prefix string, scope int, id string) (OpenResult, error) {
	s.ops.Lock()
	defer s.ops.Unlock()
	if _, err := s.current(prefix); err != nil {
		return OpenResult{}, err
	}
	next, err := Open(s.source, s.options)
	if err != nil {
		return OpenResult{}, err
	}
	doc, ok := next.doc.(navigationDocument)
	if !ok {
		_ = next.Close()
		return OpenResult{}, errNavigationUnavailable
	}
	if err := doc.SelectVersion(scope, id); err != nil {
		_ = next.Close()
		return OpenResult{}, err
	}
	return s.publish(next)
}
