package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const maxFallbackFontBytes = 32 << 20

type fallbackFontCache interface {
	Load(ctx context.Context) ([]byte, error)
	Store(ctx context.Context, data []byte) error
}

var (
	errFallbackFontSize = errors.New("回退字体文件为空或超过 32 MiB")
	errFallbackHTTP     = errors.New("字体下载失败")
)

// 缓存数据和下载数据均须通过校验，损坏的缓存不阻止重新下载。
func loadViewerFallback(ctx context.Context, cache fallbackFontCache, client *http.Client, urls []string, accept func([]byte) error) error {
	if cache != nil {
		data, err := cache.Load(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && len(data) > 0 && len(data) <= maxFallbackFontBytes && accept(data) == nil {
			return nil
		}
	}
	var failures []error
	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := func() error {
			downloadCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("%w: HTTP %s", errFallbackHTTP, resp.Status)
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, maxFallbackFontBytes+1))
			if err != nil {
				return err
			}
			if len(data) == 0 || len(data) > maxFallbackFontBytes {
				return errFallbackFontSize
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := accept(data); err != nil {
				return err
			}
			if cache != nil {
				if err := cache.Store(ctx, data); err != nil {
					slog.Warn("缓存默认字体失败", "error", err)
				}
			}
			return nil
		}()
		if err == nil {
			return nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", url, err))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(failures...)
}
