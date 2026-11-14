package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testFontCache struct {
	data []byte
	err  error
}

func (c *testFontCache) Load(context.Context) ([]byte, error) { return c.data, c.err }
func (c *testFontCache) Store(_ context.Context, data []byte) error {
	c.data = append([]byte(nil), data...)
	return c.err
}

func TestFontCacheHitAndCorruptCacheRecovery(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte("valid"))
	}))
	defer server.Close()
	cache := &testFontCache{data: []byte("bad")}
	accept := func(data []byte) error {
		if string(data) != "valid" {
			return errors.New("无效字体")
		}
		return nil
	}
	if err := loadViewerFallback(t.Context(), cache, server.Client(), []string{server.URL}, accept); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || string(cache.data) != "valid" {
		t.Fatalf("损坏缓存未被更新: 请求=%d 缓存=%q", requests, cache.data)
	}
	if err := loadViewerFallback(t.Context(), cache, server.Client(), []string{server.URL}, accept); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("有效缓存不应再次下载")
	}
}

func TestFontDownloadAlternatesAndCacheFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.Error(w, "failed", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("font"))
	}))
	defer server.Close()
	cache := &testFontCache{err: errors.New("缓存不可用")}
	err := loadViewerFallback(t.Context(), cache, server.Client(), []string{server.URL + "/bad", server.URL + "/good"}, func(data []byte) error {
		if string(data) != "font" {
			return errors.New("无效字体")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("缓存不可用时仍应使用备选下载: %v", err)
	}
}

func TestFontDownloadPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := loadViewerFallback(ctx, &testFontCache{}, http.DefaultClient, []string{"https://example.invalid"}, func([]byte) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应保留取消错误: %v", err)
	}
}
