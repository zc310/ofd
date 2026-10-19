package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"golang.org/x/image/font/gofont/goregular"
)

type memoryFontCache struct {
	data                []byte
	loads, stores       int
	loadError, putError error
}

func (c *memoryFontCache) Load(context.Context) ([]byte, error) {
	c.loads++
	return bytes.Clone(c.data), c.loadError
}

func (c *memoryFontCache) Store(_ context.Context, data []byte) error {
	c.stores++
	if c.putError == nil {
		c.data = bytes.Clone(data)
	}
	return c.putError
}

func TestFallbackFontTriesAlternateAfterHTTPOrParseFailure(t *testing.T) {
	for _, failure := range []string{"http", "font", "empty"} {
		t.Run(failure, func(t *testing.T) {
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.Path
				if r.URL.Path == "/primary" {
					switch failure {
					case "http":
						http.Error(w, "missing font", http.StatusNotFound)
					case "font":
						_, _ = w.Write([]byte("invalid font"))
					}
					return
				}
				_, _ = w.Write(goregular.TTF)
			}))
			defer server.Close()
			cache := &memoryFontCache{}
			family := t.Name()
			defer render.UnregisterFallbackFont(family)
			warning, err := loadFallbackFont(context.Background(), cache, server.Client(), []string{server.URL + "/primary", server.URL + "/alternate"}, func(data []byte) error {
				return render.RegisterFallbackFont(data, family, render.FontRegular)
			})
			if err != nil || warning != nil {
				t.Fatalf("load: %v, cache: %v", err, warning)
			}
			if len(requests) != 2 || <-requests != "/primary" || <-requests != "/alternate" || cache.stores != 1 || !bytes.Equal(cache.data, goregular.TTF) {
				t.Fatalf("request count = %d, stores = %d", len(requests), cache.stores)
			}
		})
	}
}

func TestFallbackFontReusesValidatedCacheWithoutNetwork(t *testing.T) {
	cache := &memoryFontCache{data: []byte("cached font")}
	registered := 0
	warning, err := loadFallbackFont(context.Background(), cache, nil, nil, func(data []byte) error {
		registered++
		if !bytes.Equal(data, cache.data) {
			t.Fatal("cached bytes were changed")
		}
		return nil
	})
	if err != nil || warning != nil || registered != 1 || cache.loads != 1 || cache.stores != 0 {
		t.Fatalf("error = %v, warning = %v, register = %d, cache = %#v", err, warning, registered, cache)
	}
}

func TestFallbackFontReplacesCorruptCacheAndReportsStoreFailure(t *testing.T) {
	for _, storeFails := range []bool{false, true} {
		cache := &memoryFontCache{data: []byte("corrupt")}
		if storeFails {
			cache.putError = errors.New("quota exceeded")
		}
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			_, _ = w.Write([]byte("valid"))
		}))
		warning, err := loadFallbackFont(context.Background(), cache, server.Client(), []string{server.URL}, func(data []byte) error {
			if string(data) != "valid" {
				return errors.New("invalid font")
			}
			return nil
		})
		server.Close()
		if err != nil || (warning != nil) != storeFails || requests.Load() != 1 || cache.stores != 1 {
			t.Fatalf("store failure %v: err = %v, warning = %v, requests = %d", storeFails, err, warning, requests.Load())
		}
		if !storeFails && string(cache.data) != "valid" {
			t.Fatal("invalid cache was not replaced")
		}
	}
}

func TestFallbackFontFailureNeverCachesAndCancellationIsPreserved(t *testing.T) {
	cache := &memoryFontCache{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("invalid font"))
	}))
	defer server.Close()
	register := func([]byte) error { return errors.New("invalid font") }
	if _, err := loadFallbackFont(context.Background(), cache, server.Client(), []string{server.URL}, register); err == nil || cache.stores != 0 {
		t.Fatalf("err = %v, stores = %d", err, cache.stores)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadFallbackFont(ctx, cache, server.Client(), []string{server.URL}, register); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestFallbackFontOptionIsWebOnlyAndBusyDuringDownload(t *testing.T) {
	v := newLoadedTestViewer(t)
	if len(v.fallbackFontMenuItems(false)) != 0 {
		t.Fatal("native viewer has a web font option")
	}
	if item := v.fallbackFontMenuItems(true)[0]; item.Checked || item.Disabled {
		t.Fatal("font option must start unchecked and enabled")
	}
	v.fallbackFont.downloading = true
	if !v.fallbackFontMenuItems(true)[0].Disabled {
		t.Fatal("font option allows overlapping downloads")
	}
}

func registerViewerTestFont(t *testing.T) {
	t.Helper()
	if err := render.RegisterFallbackFont(goregular.TTF, defaultFallbackFontFamily, render.FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = render.UnregisterFallbackFont(defaultFallbackFontFamily) })
}

func TestFallbackFontRefreshInvalidatesOldImagesAndPreservesNavigation(t *testing.T) {
	registerViewerTestFont(t)
	v := newLoadedTestViewer(t)
	v.currentPage = 1
	v.pageLayout.mode = viewDoublePage
	raster := image.NewRGBA(image.Rect(0, 0, 2, 2))
	v.pageImages.AddWeighted(0, raster, 16)
	oldSlot := v.pageSlots[0]
	oldSlot.rendering.Store(true)
	operation, generation := v.operation.Load(), v.thumbnailGeneration.Load()
	v.fallbackFont = fallbackFontState{enabled: true, ready: true}
	v.exporting = true
	v.syncDefaultFallbackFont()
	if v.fallbackFont.applied || v.operation.Load() != operation {
		t.Fatal("font changed during export")
	}
	v.finishExport()
	if !v.fallbackFont.applied || v.operation.Load() == operation || v.thumbnailGeneration.Load() == generation {
		t.Fatal("font change did not invalidate stale renders")
	}
	if v.currentPage != 1 || v.totalPages != 3 || v.pageLayout.mode != viewDoublePage {
		t.Fatal("font refresh changed document navigation")
	}
	if v.pageImages.Len() != 0 || v.thumbnails.Len() != 0 || v.pageSlots[0] == oldSlot || v.pageSlots[0].rendering.Load() {
		t.Fatal("old images or render flags survived the font change")
	}
	v.setDefaultFallbackFont(false)
	if v.fallbackFont.applied || fyne.CurrentApp().Preferences().Bool(fallbackFontPreferenceKey) {
		t.Fatal("disabling the option did not persist")
	}
	v.setDefaultFallbackFont(true)
	if !v.fallbackFont.applied || v.fallbackFont.downloading {
		t.Fatal("re-enabling a loaded font started a new download")
	}
}

func TestFallbackFontDownloadFailureAllowsRetry(t *testing.T) {
	v := newLoadedTestViewer(t)
	v.fallbackFont = fallbackFontState{enabled: true, downloading: true}
	v.finishFallbackFontDownload(errors.New("download failed"))
	if v.fallbackFont.ready || v.fallbackFont.enabled || v.fallbackFont.downloading || v.fallbackFontMenuItems(true)[0].Disabled {
		t.Fatal("failed download left option stuck")
	}
}

// Set OFD_VIEWER_TEST_FALLBACK_FONT to a downloaded CJK font to additionally
// exercise the supplied CDN font against documents with missing/embedded fonts.
func TestFallbackFontRendersAndExportsDocuments(t *testing.T) {
	data := goregular.TTF
	if path := os.Getenv("OFD_VIEWER_TEST_FALLBACK_FONT"); path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := render.RegisterFallbackFont(data, defaultFallbackFontFamily, render.FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = render.UnregisterFallbackFont(defaultFallbackFontFamily) })
	for _, fixture := range []string{"ofdrw/ano.ofd", "ofdrw/intro.ofd"} {
		t.Run(fixture, func(t *testing.T) {
			documents := openTestDocuments(t, fixture, color.Transparent)
			for _, document := range documents {
				t.Cleanup(document.Close)
			}
			if err := applyDefaultFallbackFont(documents, true); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"png", "pdf", "txt"} {
				var output bytes.Buffer
				if err := exportDocumentsToWriter(documents, &output, format, 36, color.White, defaultFallbackFontFamily); err != nil {
					t.Fatalf("%s: %v", format, err)
				}
				if output.Len() == 0 {
					t.Fatalf("%s export is empty", format)
				}
			}
		})
	}
}

func TestDocumentSessionClosesFallbackFontResources(t *testing.T) {
	registerViewerTestFont(t)
	document := render.NewDocument(color.Transparent, &parser.Document{})
	if err := applyDefaultFallbackFont([]*render.Document{document}, true); err != nil {
		t.Fatal(err)
	}
	closer := &viewerDocumentCloser{ofd: &parser.OFD{}, documents: []*render.Document{document}}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := document.UseFallbackFont(defaultFallbackFontFamily); err == nil {
		t.Fatal("render font resources were left open")
	}
}
