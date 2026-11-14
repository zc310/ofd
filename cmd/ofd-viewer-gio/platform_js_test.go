//go:build js

package main

import (
	"context"
	"errors"
	"testing"
)

func TestBrowserUnsupportedOperationsAndCancellation(t *testing.T) {
	if _, err := chooseExportFile("a.ofd", "pdf", 1); err == nil {
		t.Fatal("浏览器导出应明确报告未适配")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := exportFileInput(ctx, "", "", exportOptions{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消的导出应返回 context.Canceled，实际为 %v", err)
	}
}
