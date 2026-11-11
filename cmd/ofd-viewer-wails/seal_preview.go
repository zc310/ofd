package main

import (
	"context"
	"errors"
	"strings"

	"github.com/zc310/ofd/internal/reader/preview"
)

var errSealPreviewOptions = errors.New("印章预览参数无效：需要有效的文档体、签名标识及签章索引")

// SignatureSealPreview 返回经过格式、大小和访问权限检查的独立印章图片。
func (d *Desktop) SignatureSealPreview(ctx context.Context, prefix string, scope int, id string, stampIndex int) (preview.Image, error) {
	if scope < 0 || strings.TrimSpace(id) == "" || stampIndex < 0 {
		return preview.Image{}, errSealPreviewOptions
	}
	return d.document.SignatureSealPreview(ctx, prefix, scope, id, stampIndex)
}
