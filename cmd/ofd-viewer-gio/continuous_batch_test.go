package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

type batchWeightedScene struct {
	*canvas.Canvas

	weight int64
}

func (s *batchWeightedScene) Weight() int64   { return s.weight }
func (s *batchWeightedScene) Width() float64  { return s.W }
func (s *batchWeightedScene) Height() float64 { return s.H }

func TestPageBatchKeepsCurrentPageAndReportsIndividualErrors(t *testing.T) {
	m, err := openDocument(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	first := m.scene
	items, err := loadPageBatch(context.Background(), m, []int{1, 9, 0, 1})
	if err != nil || len(items) != 4 {
		t.Fatalf("批量加载失败: %v", err)
	}
	if items[0].scene == nil || !errors.Is(items[1].err, errPageRange) || items[2].scene != first || items[3].scene != items[0].scene {
		t.Fatalf("批次页面结果错误: %+v", items)
	}
	if m.current != 0 || m.scene != first {
		t.Fatal("预加载不应改变当前页")
	}
	if m.scenes.Len() > pageSceneCacheEntries || m.scenes.Weight() > pageSceneCacheBytes {
		t.Fatal("页面缓存超出预算")
	}
}

func TestPageBatchLimitsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadPageBatch(ctx, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
	if _, err := loadPageBatch(context.Background(), nil, nil); !errors.Is(err, errNoDocument) {
		t.Fatalf("缺少文档未报告错误: %v", err)
	}
	m := &documentModel{pages: make([]viewerPage, 1), scene: &batchWeightedScene{Canvas: canvas.New(1, 1), weight: pageSceneCacheBytes/2 + 1}}
	if _, err := loadPageBatch(context.Background(), m, make([]int, pageSceneCacheEntries+1)); !errors.Is(err, errBatchSize) {
		t.Fatalf("批次条目数未限制: %v", err)
	}
	items, err := loadPageBatch(context.Background(), m, []int{0, 0})
	if err != nil || items[0].scene == nil || !errors.Is(items[1].err, errBatchWeight) || items[1].scene != nil {
		t.Fatalf("批次重量未限制: %+v, %v", items, err)
	}
}

func TestWorkerPageBatchUsesSameDocumentSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	requests, results := make(chan request, 1), make(chan result, 1)
	done := make(chan struct{})
	go func() { defer close(done); runWorker(ctx, requests, results, func() {}, nil) }()
	defer func() { cancel(); <-done }()
	requests <- request{filename: createTestDocument(t)}
	first := <-results
	if first.err != nil || first.session == nil {
		t.Fatalf("打开失败: %v", first.err)
	}
	lease := first.session.acquire()
	if lease == nil {
		t.Fatal("无法取得租约")
	}
	defer lease.release()
	requests <- request{pages: []int{0, 1}}
	select {
	case response := <-results:
		if response.err != nil || response.session != first.session || response.page != 0 || len(response.pages) != 2 || response.pages[1].scene == nil {
			t.Fatalf("批次响应错误: %+v", response)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("页面批次未完成")
	}
}
