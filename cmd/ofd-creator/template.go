package main

import (
	"fmt"
	"io"
	"strings"
)

func runTemplate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = fmt.Fprintln(stdout, "用法：ofd-creator template render -i template.yaml --data data.json -o result.ofd [--check] [--validate]")
		return exitOK
	}
	if len(args) == 0 || !strings.EqualFold(args[0], "render") {
		_, _ = fmt.Fprintln(stderr, "ofd-creator template: 需要 render 子命令；使用 template --help 查看用法")
		return exitUsage
	}
	return runCreate(args[1:], stdout, stderr, true)
}
