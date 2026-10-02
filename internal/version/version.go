// Package version 提供全仓库唯一的版本号来源。
//
// 构建期由 Makefile 的 VERSION 注入到 Version：
//
//	-ldflags "-X github.com/zc310/ofd/internal/version.Version=$(VERSION)"
//
// 注入对所有包级字符串变量生效，不必为每个二进制单独指定符号路径。命令行程序的
// --version、查看器的关于对话框、Android APK 版本名，以及写进报告和转换产物的
// 工具版本，全部读同一个值。
//
// Version 是裸版本号，不带 v 前缀：前缀属于展示层（查看器的关于对话框），
// 报告和 OFD 元数据里的版本字段沿用无前缀形式。
package version

// Version 是发行版本号。
//
// 未经 Makefile 构建时（go run、go test、IDE 内运行）使用这个兜底值，因此它必须
// 与 Makefile 的 VERSION 保持一致——TestVersionMatchesMakefile 会守住这一点。
var Version = "0.1.4"
