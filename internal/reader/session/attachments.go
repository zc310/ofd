package session

import (
	"bytes"
	"context"
	"errors"
	"unicode/utf8"

	"github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

var errTextPreviewSize = errors.New("文本附件超过 1 MiB 预览限制")
var errTextPreviewEncoding = errors.New("仅支持 UTF-8 纯文本附件，不能包含二进制控制字符")

// MediaExportData 读取供原生保存的资源；预览权限不能代替导出权限。
func (s *Session) MediaExportData(ctx context.Context, scope int, id uint64) ([]byte, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) ([]byte, error) {
		if err := s.doc.CheckDocumentPermission(reader.PermissionExport, scope); err != nil {
			return nil, err
		}
		data, err := s.doc.MediaData(scope, id, 64<<20)
		if err != nil {
			return nil, err
		}
		if err := s.doc.CheckDocumentPermission(reader.PermissionExport, scope); err != nil {
			return nil, err
		}
		return data, nil
	})
}

// AttachmentData 在导出权限检查后读取附件，供原生宿主保存，最多 64 MiB。
func (s *Session) AttachmentData(ctx context.Context, scope int, id string) ([]byte, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) ([]byte, error) {
		return s.doc.AttachmentData(scope, id, 64<<20)
	})
}

// AttachmentTextPreview 按导出权限读取至多 1 MiB 的 UTF-8 文本，不解析标记或执行内容。
func (s *Session) AttachmentTextPreview(ctx context.Context, scope int, id string) (string, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) (string, error) {
		data, err := s.doc.AttachmentData(scope, id, 1<<20)
		if err != nil {
			return "", err
		}
		if len(data) > 1<<20 {
			return "", errTextPreviewSize
		}
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(data) {
			return "", errTextPreviewEncoding
		}
		for _, char := range string(data) {
			if (char < 32 && char != '\n' && char != '\r' && char != '\t') || (char >= 0x7f && char <= 0x9f) {
				return "", errTextPreviewEncoding
			}
		}
		return string(data), nil
	})
}

// MediaImagePreview 读取不超过 8 MiB、2500 万像素的光栅图片；额外格式返回静态 PNG。
func (s *Session) MediaImagePreview(ctx context.Context, scope int, id uint64) (preview.Image, error) {
	return limited(ctx, s, s.listMetadata, func(ctx context.Context) (preview.Image, error) {
		if err := s.doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return preview.Image{}, err
		}
		data, err := s.doc.MediaData(scope, id, preview.MaxImageBytes)
		if err != nil {
			return preview.Image{}, err
		}
		result, err := preview.Raster(ctx, data)
		if err != nil {
			return preview.Image{}, err
		}
		if err := s.doc.CheckDocumentPermission(reader.PermissionAccess, scope); err != nil {
			return preview.Image{}, err
		}
		return result, nil
	})
}
