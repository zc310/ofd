package session

import (
	"context"
	"strings"

	"github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

// MediaThumbnail 在读取前后检查访问权限，最多同时处理两张资源或印章缩略图。
func (s *Session) MediaThumbnail(ctx context.Context, scope int, id uint64) (preview.Thumbnail, error) {
	return s.thumbnail(ctx, scope, func(_ context.Context) ([]byte, error) { return s.doc.MediaData(scope, id, preview.MaxImageBytes) })
}

// SignatureSealThumbnail 生成印章缩略图，不复用签名清单的权限快照。
func (s *Session) SignatureSealThumbnail(ctx context.Context, scope int, id string, index int) (preview.Thumbnail, error) {
	return s.thumbnail(ctx, scope, func(ctx context.Context) ([]byte, error) {
		data, kind, err := s.doc.SignatureSeal(scope, id, index)
		if err == nil && strings.EqualFold(kind, "ofd") {
			return preview.OFDSeal(ctx, data, 256)
		}
		return data, err
	})
}

func (s *Session) thumbnail(ctx context.Context, scope int, read func(context.Context) ([]byte, error)) (preview.Thumbnail, error) {
	return limited(ctx, s, s.listMetadata, func(ctx context.Context) (preview.Thumbnail, error) {
		if err := s.doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return preview.Thumbnail{}, err
		}
		data, err := read(ctx)
		if err != nil {
			return preview.Thumbnail{}, err
		}
		thumbnail, err := preview.MakeThumbnail(ctx, data)
		if err != nil {
			return preview.Thumbnail{}, err
		}
		if err := s.doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return preview.Thumbnail{}, err
		}
		return thumbnail, nil
	})
}

// MediaThumbnail 读取指定会话的有界资源缩略图。
func (s *Service) MediaThumbnail(ctx context.Context, prefix string, scope int, id uint64) (preview.Thumbnail, error) {
	session, err := s.current(prefix)
	if err != nil {
		return preview.Thumbnail{}, err
	}
	return session.MediaThumbnail(ctx, scope, id)
}

// SignatureSealThumbnail 读取指定会话的有界印章缩略图。
func (s *Service) SignatureSealThumbnail(ctx context.Context, prefix string, scope int, id string, index int) (preview.Thumbnail, error) {
	session, err := s.current(prefix)
	if err != nil {
		return preview.Thumbnail{}, err
	}
	return session.SignatureSealThumbnail(ctx, scope, id, index)
}
