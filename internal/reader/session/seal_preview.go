package session

import (
	"context"
	"strings"

	"github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

// SignatureSealPreview 按所属文档体的访问权限读取原始印章图片；不代表验签结论。
// PNG/JPEG 检查原图；OFD 子文档第一页渲染为长边最多 2048 像素的 PNG。
func (s *Session) SignatureSealPreview(ctx context.Context, scope int, id string, stampIndex int) (preview.Image, error) {
	return limited(ctx, s, s.pageMetadata, func(ctx context.Context) (preview.Image, error) {
		if err := s.doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return preview.Image{}, err
		}
		data, kind, err := s.doc.SignatureSeal(scope, id, stampIndex)
		if err != nil {
			return preview.Image{}, err
		}
		if err := ctx.Err(); err != nil {
			return preview.Image{}, err
		}
		if strings.EqualFold(kind, "ofd") {
			data, err = preview.OFDSeal(ctx, data, 2048)
			if err != nil {
				return preview.Image{}, err
			}
		}
		result, err := preview.PNGJPEG(data)
		if err != nil {
			return preview.Image{}, err
		}
		if err := s.doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return preview.Image{}, err
		}
		return result, nil
	})
}

// SignatureSealPreview 查询指定会话的受限印章图片预览，旧会话不能访问新文档。
func (s *Service) SignatureSealPreview(ctx context.Context, prefix string, scope int, id string, stampIndex int) (preview.Image, error) {
	session, err := s.current(prefix)
	if err != nil {
		return preview.Image{}, err
	}
	return session.SignatureSealPreview(ctx, scope, id, stampIndex)
}
