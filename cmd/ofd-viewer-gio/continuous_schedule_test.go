package main

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestContinuousMetadataKeepsPageOrderAndCurrentScene(t *testing.T) {
	m, err := openDocument(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	first := m.scene
	metadata, err := readContinuousMetadata(context.Background(), m)
	if err != nil || len(metadata) != 2 {
		t.Fatalf("读取尺寸元数据失败: %v", err)
	}
	for i, page := range metadata {
		if page.err != nil || page.size.width != 210 || page.size.height != 297 {
			t.Fatalf("第 %d 页尺寸错误: %+v", i+1, page)
		}
	}
	if m.current != 0 || m.scene != first {
		t.Fatal("读取元数据不能改变当前页")
	}
	bad := readPageMetadata(viewerPage{})
	if bad.err == nil || bad.size != (continuousPageSize{210, 297}) {
		t.Fatalf("无效页面应保留占位尺寸和错误: %+v", bad)
	}
}

func TestContinuousMetadataCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readContinuousMetadata(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
	if _, err := readContinuousMetadata(context.Background(), nil); !errors.Is(err, errNoDocument) {
		t.Fatalf("缺少文档未报告错误: %v", err)
	}
}

func TestVisiblePageBatchSchedulesOnlyMissingVisiblePages(t *testing.T) {
	sizes := make([]continuousPageSize, 10000)
	metadata := make([]continuousPageMetadata, len(sizes))
	for i := range sizes {
		sizes[i] = continuousPageSize{100, 200}
		metadata[i].size = sizes[i]
	}
	g, ok := buildContinuousGeometry(sizes, 200, 1, 10)
	if !ok {
		t.Fatal("建立连续布局失败")
	}
	metadata[5002].err = errInvalidPageSize
	ready := func(page int) bool { return page == 5001 }
	got := visiblePageBatch(g, metadata, 5000*210, 5010*210, ready)
	if !slices.Equal(got, []int{5000, 5003, 5004, 5005}) {
		t.Fatalf("可见页调度错误: %v", got)
	}
	if got := visiblePageBatch(g, metadata, 200, 210, ready); len(got) != 0 {
		t.Fatalf("页间空隙不应加载页面: %v", got)
	}
}

func TestWorkerReturnsMetadataWithoutChangingCurrentPage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	requests, results := make(chan request, 1), make(chan result, 1)
	done := make(chan struct{})
	go func() { defer close(done); runWorker(ctx, requests, results, func() {}, nil) }()
	defer func() { cancel(); <-done }()
	requests <- request{filename: createTestDocument(t)}
	first := <-results
	if first.err != nil {
		t.Fatal(first.err)
	}
	requests <- request{metadata: true}
	response := <-results
	if response.err != nil || response.page != first.page || response.scene != first.scene || response.session != first.session || len(response.metadata) != 2 {
		t.Fatalf("元数据响应错误: %+v", response)
	}
}
