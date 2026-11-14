//go:build !js && !windows

package main

import (
	"context"
	"os/exec"
	"runtime"
)

func openWebURL(ctx context.Context, uri string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	// 地址始终作为单个参数传递，不通过 shell 执行文档内容。
	return exec.CommandContext(ctx, command, uri).Run()
}
