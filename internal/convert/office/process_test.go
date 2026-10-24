package office

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const processHelperEnv = "OFD_OFFICE_PROCESS_HELPER"
const processMarkerEnv = "OFD_OFFICE_PROCESS_MARKER"

// 在解析 LibreOffice 参数前接管测试二进制，跨平台模拟保留子进程的 soffice。
func TestMain(m *testing.M) {
	if mode := os.Getenv(processHelperEnv); mode != "" {
		marker := os.Getenv(processMarkerEnv)
		if mode == "child" {
			if err := os.WriteFile(marker+".ready", nil, 0600); err != nil {
				os.Exit(2)
			}
			time.Sleep(1500 * time.Millisecond)
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
		child := exec.Command(os.Args[0], "-test.run=^$")
		child.Env = append(os.Environ(), processHelperEnv+"=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestConvertToPDFCancellationTerminatesDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-finished")
	t.Setenv(processHelperEnv, "parent")
	t.Setenv(processMarkerEnv, marker)
	input := writeOfficeTestInput(t)
	tempRoot := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	var conversionErr error
	go func() {
		defer close(done)
		_, conversionErr = ConvertToPDF(ctx, input, Options{Soffice: os.Args[0], TempDir: tempRoot})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	readyDeadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker + ".ready"); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-done:
			t.Fatalf("子进程就绪前转换已退出: %v", conversionErr)
		default:
		}
		if time.Now().After(readyDeadline) {
			t.Fatal("子进程未就绪")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if conversionErr != context.Canceled {
		t.Fatalf("取消错误 = %v，期望原始 context.Canceled", conversionErr)
	}
	time.Sleep(1700 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("取消转换后子进程仍完成了写入: %v", err)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("取消后临时文件未清理: %v, %v", entries, err)
	}
}
