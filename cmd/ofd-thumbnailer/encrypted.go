package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/rasterizer"
)

// encryptedThumbnailSVG 仅使用矢量轮廓，不依赖系统字体或文档内的图片资源。
//
//go:embed encrypted.svg
var encryptedThumbnailSVG []byte

func generateEncryptedThumbnail(ctx context.Context, input, output string, size int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scene, err := canvas.ParseSVG(bytes.NewReader(encryptedThumbnailSVG))
	if err != nil {
		return fmt.Errorf("parse encrypted thumbnail: %w", err)
	}
	resolution, err := thumbnailResolution(scene.W, scene.H, size)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	img := rasterizer.Draw(scene, canvas.Resolution(resolution), canvas.DefaultColorSpace)
	return saveThumbnail(ctx, input, output, img)
}
