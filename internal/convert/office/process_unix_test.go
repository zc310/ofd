//go:build !windows

package office

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

	"github.com/zc310/ofd/internal/testutil"
)

// 即使已经生成有效 PDF，继承输出管道的子进程也不能让转换无限等待或静默成功。
func TestConvertToPDFBoundsWaitForInheritedOutputPipe(t *testing.T) {
	if err := ensureUnixTestShell(); err != nil {
		t.Skip(err)
	}
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("测试需要 setsid 创建脱离进程组的子进程")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "valid.pdf")
	if err := os.WriteFile(pdfPath, testutil.MinimalPDF(nil, 100, 100), 0600); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(dir, "child.pid")
	tool := writeFakeSoffice(t, "out=\"\"; previous=\"\"; for arg in \"$@\"; do if [ \"$previous\" = \"--outdir\" ]; then out=\"$arg\"; fi; previous=\"$arg\"; done\n"+
		"cp "+shellQuote(pdfPath)+" \"$out/input.pdf\"\n"+
		"setsid sh -c 'sleep 4' &\nprintf '%s' \"$!\" > "+shellQuote(pidPath))
	t.Cleanup(func() {
		data, err := os.ReadFile(pidPath)
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
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	_, err := ConvertToPDF(ctx, writeOfficeTestInput(t), Options{Soffice: tool})
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("继承管道未返回 WaitDelay 错误: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("管道等待过长: %s", elapsed)
	}
}
