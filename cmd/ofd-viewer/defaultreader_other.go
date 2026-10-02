//go:build !linux || flatpak || android

package main

import "errors"

// 非 Linux 平台，以及 Linux 上的 Flatpak 与 Android，都不探测也不设置默认阅读器。
//
// 原因与 desktopentry_other.go 相同，这里不再重复：三者的文件关联都由各自的
// 打包或安装流程负责。构建条件必须与 defaultreader_linux.go 的
// linux && !flatpak && !android 严格互补，否则两个文件会在同一构建里同时编译，
// 类型和函数重复定义。
//
// 状态报 Unavailable 而不是 NotDefault：菜单项据此禁用并说明原因，也不会在启动
// 时弹一个必然失败的提示。

func detectDefaultReader() defaultReaderState {
	return defaultReaderUnavailable
}

func setDefaultReader() error {
	return errors.New("当前系统不支持由应用自动修改文件关联，请在系统设置中手动把 OFD Viewer 设为 .ofd 文件的默认应用")
}
