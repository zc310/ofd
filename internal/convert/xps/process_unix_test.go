//go:build !windows

package xps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConvertToPDFBoundsWaitForInheritedOutputPipe(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("测试需要 setsid 创建脱离进程组的子进程")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.xps")
	if err := os.WriteFile(input, []byte("xps"), 0600); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "child.pid")
	// 子进程独占进程组并继承输出管道；即使父进程退出，管道仍然打开。
	tool := writeFakeXpstopdf(t, "setsid sh -c 'sleep 30' &\nprintf '%s' \"$!\" > "+shellQuote(pidFile))
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			t.Error(err)
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			t.Errorf("无效子进程 PID: %q", data)
			return
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := ConvertToPDF(ctx, input, Options{Xpstopdf: tool})
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("继承管道未返回 WaitDelay 错误: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("管道等待过长: %s", elapsed)
	}
}
