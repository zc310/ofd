package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestPageOperationPanicIncludesLocationAndLogsStack(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var model *documentModel
	err := handleRequest(&model, request{choose: true}, panicChoosingDocument)
	if !errors.Is(err, errPageOperation) {
		t.Fatalf("页面异常分类丢失: %v", err)
	}
	if !strings.Contains(err.Error(), "panicChoosingDocument") || !strings.Contains(err.Error(), "page_error_test.go:") {
		t.Fatalf("异常未保留发生位置: %v", err)
	}
	if !strings.Contains(logs.String(), "stack=") || !strings.Contains(logs.String(), "panicChoosingDocument") {
		t.Fatalf("诊断日志缺少异常堆栈: %s", &logs)
	}
}

func panicChoosingDocument() (string, error) {
	panic("测试页面加载异常")
}

func TestContinuousPageFailureShowsSpecificError(t *testing.T) {
	v := viewer{busy: true}
	v.continuous.reset(0)
	v.continuous.pending = continuousPagesRequest
	v.acceptWorkerResponse(result{pages: []pageResult{{page: 1, err: errPageOperation}}}, func() {}, func(string) {})
	if v.busy || !strings.Contains(v.status, "第 2 页") || !strings.Contains(v.status, errPageOperation.Error()) {
		t.Fatalf("连续页失败未展示具体原因: busy=%v status=%q", v.busy, v.status)
	}
	if _, failed := v.continuous.failures.Get(1); !failed {
		t.Fatal("失败页应保留占位状态")
	}
}
