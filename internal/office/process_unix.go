//go:build !windows

package office

import (
	"os/exec"
	"syscall"
)

// processGroupAttr 让子进程拥有独立进程组，便于超时时结束整棵进程树。
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup 结束整个进程组，确保 LibreOffice 派生的 soffice.bin 一并退出。
func killProcessGroup(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		return command.Process.Kill()
	}
	// 负号表示向整个进程组发送信号。
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return command.Process.Kill()
	}
	return nil
}
