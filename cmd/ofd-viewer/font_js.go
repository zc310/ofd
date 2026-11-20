//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/text"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/render"
	fontformat "github.com/zc310/ofd/internal/thirdparty/font"
)

const viewerFallbackFamily = "OFD Gio Noto Sans SC"

var viewerFallbackReady atomic.Bool //nolint:gochecknoglobals // 字体异步注册后由文档工作线程读取。

func configureViewerFont(th *material.Theme) error {
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return nil
}

func startViewerFont(ctx context.Context, wake func()) (<-chan viewerFontResult, <-chan struct{}) {
	results := make(chan viewerFontResult, 1)
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		var faces []font.FontFace
		err := loadViewerFallback(ctx, indexedDBFontCache{}, http.DefaultClient, []string{
			"https://cdn.jsdelivr.net/npm/@reogrid/font-sc@2.0.0/NotoSansSC-Regular.ttf",
			"https://cdn.jsdelivr.net/npm/@fontsource/noto-sans-sc/files/noto-sans-sc-chinese-simplified-400-normal.woff2",
		}, func(data []byte) error {
			sfnt, err := fontformat.ToSFNT(data)
			if err != nil {
				return err
			}
			parsed, err := opentype.ParseCollection(sfnt)
			if err != nil {
				return fmt.Errorf("解析界面字体: %w", err)
			}
			if err := render.RegisterFallbackFont(data, viewerFallbackFamily, render.FontRegular); err != nil {
				return fmt.Errorf("注册文档回退字体: %w", err)
			}
			faces = parsed
			viewerFallbackReady.Store(true)
			return nil
		})
		results <- viewerFontResult{faces: faces, err: err}
		wake()
	}()
	return results, ready
}

func applyViewerFallback(doc *render.Document) error {
	if !viewerFallbackReady.Load() {
		return nil
	}
	return doc.UseFallbackFont(viewerFallbackFamily)
}
