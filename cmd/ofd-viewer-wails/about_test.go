package main

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/version"
)

// 关于窗口必须读取发行版本，与命令行保持一致，且不依赖文档或图形窗口。
func TestAboutVersionUsesInjectedReleaseAndMatchesCLI(t *testing.T) {
	desktop := &Desktop{}
	if got := desktop.Version(); got != version.Version || got == "" {
		t.Fatalf("关于版本 = %q，发行版本 = %q", got, version.Version)
	}
	var stdout, stderr bytes.Buffer
	if code := command([]string{"--version"}, &stdout, &stderr, func(string) error {
		t.Fatal("查询版本不应启动图形窗口")
		return nil
	}); code != 0 || stderr.Len() != 0 || stdout.String() != "ofd-viewer-wails v"+desktop.Version()+"\n" {
		t.Fatalf("版本不一致：code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
