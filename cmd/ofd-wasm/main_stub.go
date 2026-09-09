//go:build !js || !wasm

package main

// WebAssembly 命令没有主机模式行为。此存根用于让普通 Go 包和工具检查
// 仍然可以识别该命令包。
func main() {}
