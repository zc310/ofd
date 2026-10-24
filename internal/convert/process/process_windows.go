//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func killProcessGroup(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	// CREATE_NEW_PROCESS_GROUP 本身不提供终止整个进程树的能力。
	// taskkill /T 会同时终止转换程序派生的子进程；清理命令也必须有时限。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, "taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(command.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	kill.WaitDelay = waitDelay
	if err := kill.Run(); err != nil {
		return errors.Join(fmt.Errorf("终止转换进程树失败: %w", err), command.Process.Kill())
	}
	return nil
}
