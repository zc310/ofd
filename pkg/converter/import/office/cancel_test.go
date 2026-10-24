package office_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/converter"
)

func TestOfficeConversionPassesDeadlineToExternalProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("模拟可执行文件使用 POSIX shell")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("测试需要 sh")
	}
	program := filepath.Join(t.TempDir(), "soffice")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexec sleep 5\n"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"pdf", "ofd"} {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		temp := t.TempDir()
		started := time.Now()
		err := converter.Convert(ctx, "docx", target, []byte("input"), io.Discard,
			converter.WithSoffice(program), converter.WithOfficeTimeout(2*time.Second), converter.WithTempDir(temp))
		cancel()
		if err != context.DeadlineExceeded || time.Since(started) > time.Second {
			t.Fatalf("Office→%s 未响应调用方超时: %v elapsed=%s", target, err, time.Since(started))
		}
		entries, err := os.ReadDir(temp)
		if err != nil || len(entries) != 0 {
			t.Fatalf("取消后临时文件未清理: %v %v", entries, err)
		}
	}
}
