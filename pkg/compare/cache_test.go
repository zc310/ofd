package compare

import (
	"context"
	"errors"
	"image"
	"reflect"
	"strings"
	"testing"
)

func TestBaselineCacheReusesRenderingWithoutChangingDifferences(t *testing.T) {
	a, b, c := testOFD(t, "Amount 100"), testOFD(t, "Amount 200"), testOFD(t, "Amount 300")
	r, err := CompareFiles(t.Context(), []string{a, b, c}, Options{Visual: true, MaxTotalPixels: 15000})
	if err != nil {
		t.Fatal("应只渲染一次基准页", err)
	}
	if _, err := CompareFiles(t.Context(), []string{a, b, c}, Options{Visual: true, MaxTotalPixels: 15000, BaselineCacheBytes: -1}); err == nil || !strings.Contains(err.Error(), "像素预算") {
		t.Fatal("禁用缓存后预算未计入重复渲染", err)
	}
	uncached, err := CompareFiles(t.Context(), []string{a, b, c}, Options{Visual: true, BaselineCacheBytes: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Comparisons, uncached.Comparisons) {
		t.Fatal("缓存改变了页面差异或 PNG")
	}
	if _, err := CompareFiles(t.Context(), []string{a, b, c}, Options{Visual: true, MaxTotalPixels: 15000, BaselineCacheBytes: 1}); err == nil {
		t.Fatal("超出缓存容量的页面仍被保留")
	}
}

func TestPageCacheEvictsLeastRecentlyUsedByActualBytes(t *testing.T) {
	cache := newPageCache(32)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	cache.put(1, img)
	cache.put(2, img)
	if cache.get(1) != img {
		t.Fatal("缓存未命中")
	}
	cache.put(3, img)
	if cache.bytes != 32 || cache.get(2) != nil || cache.get(1) != img || cache.get(3) != img {
		t.Fatal("未淘汰最近最少使用的页面")
	}
	cache.put(4, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	if cache.bytes != 32 || cache.get(4) != nil {
		t.Fatal("大页面绕过缓存容量")
	}
	cache.put(1, img)
	if cache.bytes != 32 || len(cache.items) != 2 {
		t.Fatal("替换重复页未正确更新容量")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := renderPage(ctx, &snapshot{cache: cache}, 1, Options{}, new(int64)); !errors.Is(err, context.Canceled) {
		t.Fatal("缓存命中忽略了取消", err)
	}
}
