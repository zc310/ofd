package xps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 使用测试二进制模拟父子进程，以便 Windows 也能验证进程树终止行为。
func TestXPSProcessHelper(t *testing.T) {
	mode := os.Getenv("OFD_XPS_PROCESS_TEST")
	if mode == "" {
		return
	}
	marker := os.Getenv("OFD_XPS_PROCESS_MARKER")
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
	child := exec.Command(os.Args[0], "-test.run=^TestXPSProcessHelper$")
	child.Env = append(os.Environ(), "OFD_XPS_PROCESS_TEST=child")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Run(); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestKillProcessGroupTerminatesDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-finished")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestXPSProcessHelper$")
	command.Env = append(os.Environ(), "OFD_XPS_PROCESS_TEST=parent", "OFD_XPS_PROCESS_MARKER="+marker)
	command.SysProcAttr = processGroupAttr()
	command.Cancel = func() error { return killProcessGroup(command) }
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
	})
	readyDeadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker + ".ready"); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(readyDeadline) {
			t.Fatal("子进程未就绪")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := killProcessGroup(command); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1700 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("终止进程组后子进程仍完成了写入: %v", err)
	}
}
