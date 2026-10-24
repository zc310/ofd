//go:build windows

package office

import (
	"os/exec"
	"syscall"
)

// processGroupAttr 在 Windows 上创建新进程组，便于统一结束子进程。
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// killProcessGroup 结束进程；Windows 上 soffice.exe 自身即为转换进程。
func killProcessGroup(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
