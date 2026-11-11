package main

import (
	"context"
	"errors"
	"strings"

	"github.com/zc310/ofd/internal/reader/preview"
)

var errThumbnailOptions = errors.New("缩略图参数无效")

// MediaThumbnail 返回原生缩小的资源图片，不向网页传输完整原图。
func (d *Desktop) MediaThumbnail(ctx context.Context, prefix string, scope int, id uint64) (preview.Thumbnail, error) {
	if scope < 0 {
		return preview.Thumbnail{}, errThumbnailOptions
	}
	return d.document.MediaThumbnail(ctx, prefix, scope, id)
}

// SignatureSealThumbnail 返回原生缩小的印章图片；大图仍通过独立预览请求读取。
func (d *Desktop) SignatureSealThumbnail(ctx context.Context, prefix string, scope int, id string, index int) (preview.Thumbnail, error) {
	if scope < 0 || strings.TrimSpace(id) == "" || index < 0 {
		return preview.Thumbnail{}, errThumbnailOptions
	}
	return d.document.SignatureSealThumbnail(ctx, prefix, scope, id, index)
}
