//go:build !linux || flatpak || android

package main

// 非 Linux 平台，以及 Linux 上的 Flatpak 与 Android，都不做桌面集成。
//
// 这些组合可能同时成立（比如 darwin 上误加 flatpak tag），所以这里的条件必须与
// desktopentry_linux.go 的 linux && !flatpak && !android 严格互补，不能写成
// !(linux && !flatpak && !android)——那样两个文件会在同一构建里同时编译，函数重复定义。
//
// macOS 用 .app bundle、Windows 用开始菜单快捷方式，都由各自的打包流程负责；Flatpak
// 由 flatpak-builder 在安装时装好三样；Android 由 APK 声明启动器图标。三者都不经
// 应用自身往用户目录写文件。
func installDesktopIntegration() {}
