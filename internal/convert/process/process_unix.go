//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

func processGroupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func killProcessGroup(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		return command.Process.Kill()
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return command.Process.Kill()
	}
	return nil
}
