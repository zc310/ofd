package main

import (
	"context"

	"github.com/zc310/ofd/internal/reader/preview"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/reader"
)

// CheckDocumentPermission 为音视频重播等操作复核所属文档体权限。
func (d *Desktop) CheckDocumentPermission(prefix string, scope int, action string) (checkPermissionReply, error) {
	err := d.document.CheckDocumentPermission(prefix, scope, reader.PermissionAction(action))
	if denied := permissionDetails(err); denied != nil {
		return checkPermissionReply{Denied: denied}, nil
	}
	return checkPermissionReply{}, err
}

// AttachmentImagePreview 返回经过格式和尺寸限制的附件图片。
func (d *Desktop) AttachmentImagePreview(ctx context.Context, prefix string, scope int, id string) (preview.Image, error) {
	return d.document.AttachmentImagePreview(ctx, prefix, scope, id)
}

// MediaPlayback 返回受限的包内音视频，不允许网页指定外部文件或 URL。
func (d *Desktop) MediaPlayback(ctx context.Context, prefix string, scope int, id, templateID uint64) (readersession.PlaybackPreview, error) {
	return d.document.MediaPlayback(ctx, prefix, scope, id, templateID)
}

// AttachmentPlayback 返回按导出权限检查的受限附件音视频。
func (d *Desktop) AttachmentPlayback(ctx context.Context, prefix string, scope int, id string) (readersession.PlaybackPreview, error) {
	return d.document.AttachmentPlayback(ctx, prefix, scope, id)
}
