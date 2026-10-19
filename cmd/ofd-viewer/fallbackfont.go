package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/zc310/ofd/internal/render"
)

// All state belongs to the Fyne event thread, including download completion.
type fallbackFontState struct {
	enabled     bool
	ready       bool
	downloading bool
	applied     bool
	cancel      context.CancelFunc
	uiFont      fyne.Resource
	uiTheme     *fallbackFontTheme
}

// Cache entries are validated by the renderer just like downloads. Store only
// successfully registered fonts; an invalid cached entry is replaced on success.
func loadFallbackFont(ctx context.Context, cache fallbackFontCache, client *http.Client, urls []string, register func([]byte) error) (cacheWarning, err error) {
	if cache != nil {
		data, cacheErr := cache.Load(ctx)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cacheErr == nil && len(data) > 0 && len(data) <= maxFallbackFontBytes {
			if err := register(data); err == nil {
				return nil, nil
			}
		}
	}
	err = downloadFallbackFont(ctx, client, urls, func(data []byte) error {
		if err := register(data); err != nil {
			return err
		}
		if cache != nil {
			cacheWarning = cache.Store(ctx, data)
		}
		return nil
	})
	return cacheWarning, err
}

// Download and parse each candidate before accepting it. An HTTP success with an
// invalid font must still fall through to the alternate source.
func downloadFallbackFont(ctx context.Context, client *http.Client, urls []string, register func([]byte) error) error {
	var failures []error
	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := func() error {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("HTTP %s", response.Status)
			}
			data, err := io.ReadAll(io.LimitReader(response.Body, maxFallbackFontBytes+1))
			if err != nil {
				return err
			}
			if len(data) == 0 || len(data) > maxFallbackFontBytes {
				return errors.New(tr("回退字体文件为空或超过 32 MiB"))
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return register(data)
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

func (v *viewer) initializeFallbackFont() {
	if !webFallbackFontSupported {
		return
	}
	if a := fyne.CurrentApp(); a != nil && a.Preferences().Bool(fallbackFontPreferenceKey) {
		v.setDefaultFallbackFont(true)
	}
}

func (v *viewer) fallbackFontMenuItems(supported bool) []*fyne.MenuItem {
	if !supported {
		return nil
	}
	label := tr("使用默认回退字体（Noto Sans SC）")
	if v.fallbackFont.downloading {
		label = tr("正在加载回退字体…")
	}
	item := fyne.NewMenuItem(label, func() { v.setDefaultFallbackFont(!v.fallbackFont.enabled) })
	item.Checked = v.fallbackFont.enabled
	item.Disabled = v.loading || v.exporting || v.fallbackFont.downloading
	return []*fyne.MenuItem{item}
}

func (v *viewer) setDefaultFallbackFont(enabled bool) {
	if v.closed.Load() || v.loading || v.exporting || v.fallbackFont.downloading {
		return
	}
	v.fallbackFont.enabled = enabled
	if a := fyne.CurrentApp(); a != nil {
		a.Preferences().SetBool(fallbackFontPreferenceKey, enabled)
	}
	if !enabled || v.fallbackFont.ready {
		v.syncDefaultFallbackFont()
		return
	}
	v.fallbackFont.downloading = true
	ctx, cancel := context.WithCancel(context.Background())
	v.fallbackFont.cancel = cancel
	go func() {
		defer cancel()
		var uiFont fyne.Resource
		warning, err := loadFallbackFont(ctx, newFallbackFontCache(), &http.Client{Timeout: 3 * time.Minute}, defaultFallbackFontURLs, func(data []byte) error {
			resource, err := prepareFallbackUIFont(data)
			if err != nil {
				return err
			}
			if err := render.RegisterFallbackFont(data, defaultFallbackFontFamily, render.FontRegular); err != nil {
				return err
			}
			uiFont = resource
			return nil
		})
		fyne.Do(func() {
			if err == nil && !v.closed.Load() {
				v.fallbackFont.uiFont = uiFont
			}
			v.finishFallbackFontDownload(err)
			if warning != nil && !v.closed.Load() && err == nil {
				dialog.ShowInformation(tr("回退字体缓存失败"), fmt.Sprintf(tr("回退字体已启用，但无法保存到浏览器缓存：%v"), warning), v.window)
			}
		})
	}()
}

func (v *viewer) finishFallbackFontDownload(err error) {
	v.fallbackFont.downloading = false
	v.fallbackFont.cancel = nil
	if v.closed.Load() {
		return
	}
	if err != nil {
		v.fallbackFont.enabled = false
		if a := fyne.CurrentApp(); a != nil {
			a.Preferences().SetBool(fallbackFontPreferenceKey, false)
		}
		dialog.ShowInformation(tr("回退字体下载失败"), err.Error(), v.window)
		return
	}
	v.fallbackFont.ready = true
	v.syncDefaultFallbackFont()
}

func applyDefaultFallbackFont(documents []*render.Document, enabled bool) error {
	for _, document := range documents {
		if enabled {
			if err := document.UseFallbackFont(defaultFallbackFontFamily); err != nil {
				return err
			}
		} else {
			document.RemoveFallbackFont(defaultFallbackFontFamily)
		}
	}
	return nil
}

// Defer font changes until loading/exporting finishes so an export sees one font
// configuration throughout. New documents receive the font before publication.
func (v *viewer) syncDefaultFallbackFont() {
	enabled := v.fallbackFont.enabled && v.fallbackFont.ready
	if v.closed.Load() || v.loading || v.exporting {
		return
	}
	v.syncFallbackFontTheme(enabled)
	if enabled == v.fallbackFont.applied {
		return
	}
	if err := applyDefaultFallbackFont(v.documents, enabled); err != nil {
		dialog.ShowInformation(tr("回退字体加载失败"), err.Error(), v.window)
		return
	}
	v.fallbackFont.applied = enabled
	if !v.hasPages() {
		return
	}
	v.refreshFontRendering()
}

func (v *viewer) refreshFontRendering() {
	// In-flight renders must not restore images created with the previous font.
	v.operation.Add(1)
	v.thumbnailGeneration.Add(1)
	v.hidePageLoading(0)
	v.pageImages = v.newPageImageCache()
	v.thumbnails = newThumbnailCache()
	v.thumbnailRendering = make([]atomic.Bool, v.totalPages)
	states := make([]pageSlotState, len(v.pageSlots))
	for i, slot := range v.pageSlots {
		states[i] = pageSlotState{aspect: slot.aspect, area: slot.area, renderable: slot.renderable}
	}
	offset := v.pageScroll.Offset
	v.createPageSlots(v.pages, states)
	v.pageScroll.ScrollToOffset(offset)
	v.thumbnailList.Refresh()
	v.updatePageWindow(v.operation.Load())
}
