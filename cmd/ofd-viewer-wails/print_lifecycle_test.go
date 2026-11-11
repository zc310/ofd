package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testPrintWindow struct {
	closes atomic.Int32
	prints atomic.Int32
	closed chan struct{}
}

func (w *testPrintWindow) Print() error { w.prints.Add(1); return nil }
func (w *testPrintWindow) Close() {
	if w.closes.Add(1) == 1 && w.closed != nil {
		close(w.closed)
	}
}

func TestPrintPreviewExpiresWithoutFurtherRequestsAndOldTimerCannotCloseNewJob(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	window := &testPrintWindow{closed: make(chan struct{})}
	if err := d.publishPrint(t.Context(), job, func(string) printWindow { return window }); err != nil {
		t.Fatal(err)
	}
	// 使用真实的回收回调，但不必等待五分钟。
	job.timer.Reset(0)
	select {
	case <-window.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("到期未主动关闭预览")
	}
	d.printMu.Lock()
	current := d.printing
	d.printMu.Unlock()
	if current != nil || window.closes.Load() != 1 {
		t.Fatal("到期未撤销任务或重复关闭窗口")
	}
	next, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	nextWindow := &testPrintWindow{}
	if err := d.publishPrint(t.Context(), next, func(string) printWindow { return nextWindow }); err != nil {
		t.Fatal(err)
	}
	d.ClosePrint(job.token)
	if nextWindow.closes.Load() != 0 || d.printing != next {
		t.Fatal("旧定时器撤销了新预览")
	}
	d.ClosePrint(next.token)
	if nextWindow.closes.Load() != 1 || next.timer.Reset(time.Hour) {
		t.Fatal("手动关闭没有停止回收定时器")
	}
	next.timer.Stop()
}

func TestPrintPreviewCancellationDuringWindowCreationDoesNotPublishLateWindow(t *testing.T) {
	for _, action := range []string{"取消", "窗口关闭"} {
		t.Run(action, func(t *testing.T) {
			d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
			job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
			if err != nil {
				t.Fatal(err)
			}
			window := &testPrintWindow{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err = d.publishPrint(ctx, job, func(string) printWindow {
				if action == "取消" {
					cancel()
				} else {
					d.releasePrint(job.token)
				}
				return window
			})
			want := errPrintExpired
			if action == "取消" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || window.closes.Load() != 1 || d.printing != nil || job.timer != nil {
				t.Fatalf("迟到窗口仍有效: %v", err)
			}
		})
	}
}

func TestPrintPreviewConcurrentCloseOnlyClosesWindowOnce(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	window := &testPrintWindow{}
	if err := d.publishPrint(t.Context(), job, func(string) printWindow { return window }); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() { d.ClosePrint(job.token) })
	}
	workers.Wait()
	if window.closes.Load() != 1 || d.printing != nil {
		t.Fatalf("并发关闭次数 = %d", window.closes.Load())
	}
}

func TestExpiredPrintResourceReadReleasesWindowAndImages(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	window := &testPrintWindow{}
	job.window, job.expires = window, time.Now().Add(-time.Second)
	d.printing = job
	response := httptest.NewRecorder()
	d.servePrint(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/reader-print/"+job.token+"/0.png", nil))
	if response.Code != http.StatusNotFound || d.printing != nil || window.closes.Load() != 1 {
		t.Fatalf("过期图片仍驻留: %d", response.Code)
	}
}

func TestPreparedPrintRechecksPermissionsAndReleasesStaleJob(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	window := &testPrintWindow{}
	if err := d.publishPrint(t.Context(), job, func(string) printWindow { return window }); err != nil {
		t.Fatal(err)
	}
	if err := d.PrintPrepared(job.token); err != nil || window.prints.Load() != 1 {
		t.Fatalf("系统打印未调用: %v", err)
	}
	if err := d.PrintPrepared("other-token"); !errors.Is(err, errPrintExpired) || window.prints.Load() != 1 || window.closes.Load() != 0 {
		t.Fatalf("无效标识影响了当前打印: %v", err)
	}
	// 模拟宿主已撤销底层会话，旧图片即便已加载也不能再打印。
	if err := d.document.CloseCurrent(opened.PagePrefix); err != nil {
		t.Fatal(err)
	}
	if err := d.PrintPrepared(job.token); err == nil || window.prints.Load() != 1 || window.closes.Load() != 1 || d.printing != nil {
		t.Fatalf("授权失效后仍打印或保留窗口: %v", err)
	}
}

func TestShutdownStopsPrintExpiryWithoutClosingHostWindowsAgain(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	job, err := d.preparePrint(t.Context(), opened.PagePrefix, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	window := &testPrintWindow{}
	if err := d.publishPrint(t.Context(), job, func(string) printWindow { return window }); err != nil {
		t.Fatal(err)
	}
	d.shutdown()
	defer job.timer.Stop()
	if d.printing != nil || job.timer.Reset(time.Hour) || window.closes.Load() != 0 {
		t.Fatal("退出时未撤销任务、停止定时器，或重复关闭宿主窗口")
	}
}
