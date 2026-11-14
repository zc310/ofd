package main

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestContinuousSessionRetireWaitsForAllLeases(t *testing.T) {
	closed := make(chan struct{})
	s := &continuousSession{close: func() { close(closed) }}
	a, b := s.acquire(), s.acquire()
	s.retire()
	if s.acquire() != nil {
		t.Fatal("退役会话不应发放新租约")
	}
	a.release()
	a.release()
	select {
	case <-closed:
		t.Fatal("仍有租约时不能关闭文档")
	default:
	}
	b.release()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("所有租约归还后文档未关闭")
	}
	s.retire()
}

func TestViewerKeepsSceneLeaseAcrossFailedRequest(t *testing.T) {
	closed := make(chan struct{})
	s := &continuousSession{close: func() { close(closed) }}
	scene := &countedScene{Canvas: canvas.New(10, 10)}
	v := viewer{scene: scene, viewScene: scene, sceneLease: s.acquire(), total: 1}
	v.acceptResponse(result{scene: scene, session: s, total: 1, err: errors.New("加载失败")}, func() {}, func(string) {})
	s.retire()
	select {
	case <-closed:
		t.Fatal("失败后仍显示页面时不能关闭文档")
	default:
	}
	if v.scene != scene || v.sceneLease == nil {
		t.Fatal("失败请求应保留页面及其租约")
	}
	v.sceneLease.release()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("页面租约归还后文档未关闭")
	}
}

func TestContinuousSessionConcurrentRetireAndReleaseClosesOnce(t *testing.T) {
	var count atomic.Int32
	closed := make(chan struct{})
	s := &continuousSession{close: func() { count.Add(1); close(closed) }}
	const n = 100
	leases := make([]*continuousLease, n)
	for i := range leases {
		leases[i] = s.acquire()
	}
	var wg sync.WaitGroup
	for _, lease := range leases {
		wg.Go(func() { lease.release(); lease.release() })
	}
	for range n {
		wg.Go(s.retire)
	}
	wg.Wait()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("并发退役后文档未关闭")
	}
	if count.Load() != 1 {
		t.Fatalf("关闭次数 = %d", count.Load())
	}
}

func TestContinuousSessionRetireDoesNotWaitForClose(t *testing.T) {
	started, unblock, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s := &continuousSession{close: func() { close(started); <-unblock; close(done) }}
	s.retire()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("关闭未开始")
	}
	close(unblock)
	<-done
}
