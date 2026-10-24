// Package process 配置外部转换命令的进程树取消和输出管道等待上限。
package process

import (
	"os/exec"
	"time"
)

const waitDelay = time.Second

// Configure 在启动前为 exec.CommandContext 创建的命令配置独立进程组和取消处理。
// Unix 取消整个进程组，Windows 通过 taskkill /T 终止进程树；输出管道最多额外等待一秒。
func Configure(command *exec.Cmd) {
	command.SysProcAttr = processGroupAttr()
	command.Cancel = func() error { return killProcessGroup(command) }
	command.WaitDelay = waitDelay
}
