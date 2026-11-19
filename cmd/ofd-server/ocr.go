package main

import (
	"context"
	"image"
	"net/http"
	"time"

	"github.com/zc310/ofd/internal/ocr"
	"github.com/zc310/ofd/pkg/converter"
)

// limitedOCREngine 的额度由整个服务共享，等待时也响应取消。
type limitedOCREngine struct {
	engine ocr.Engine
	slots  chan struct{}
}

func (e *limitedOCREngine) Recognize(ctx context.Context, img image.Image) ([]ocr.TextBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.engine.Recognize(ctx, img)
}

func ocrOptions(cfg OCRConfig, tempDir string) []converter.Option {
	var engine ocr.Engine
	switch cfg.Mode {
	case "tesseract":
		tesseract := ocr.NewTesseract(cfg.TesseractPath, cfg.Language)
		tesseract.TempDir = tempDir
		engine = tesseract
	case "http":
		client := ocr.NewHTTPClient(cfg.Endpoint, cfg.APIKey, cfg.Language)
		timeout := cfg.Timeout.Duration()
		if timeout == 0 {
			timeout = 60 * time.Second
		}
		client.Client = &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		maxConcurrency := cfg.MaxConcurrency
		if maxConcurrency == 0 {
			maxConcurrency = 4
		}
		engine = &limitedOCREngine{engine: client, slots: make(chan struct{}, maxConcurrency)}
	default:
		return nil
	}
	return []converter.Option{converter.ImageOCR(true), converter.WithImageOCREngine(engine)}
}
