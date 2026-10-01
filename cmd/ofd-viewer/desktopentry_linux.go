//go:build linux && !flatpak && !android

package main

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 桌面集成所需的三个文件内嵌进二进制。用户直接下载二进制或从压缩包里解开就能用，
// 不需要系统包管理器安装——这是发行独立二进制的前提。
//
// 桌面环境只认 XDG 数据目录（~/.local/share）下的这三个文件，"配置目录"（~/.config）
// 里的 desktop 文件不会被菜单索引到，所以位置不能改。
//
//go:embed share/io.github.zc310.ofd.desktop
//go:embed share/io.github.zc310.ofd.xml
//go:embed share/io.github.zc310.ofd.svg
var shareFiles embed.FS

const (
	desktopFileName = applicationID + ".desktop"
	mimeFileName    = applicationID + ".xml"
	iconFileName    = applicationID + ".svg"
)

// installDesktopIntegration 把桌面文件、MIME 定义和图标写入当前用户的 XDG 数据目录，
// 让应用出现在系统菜单里并能双击打开 .ofd 文件。
//
// Flatpak 与 Android 不走这条路径：Flatpak 沙箱由 flatpak-builder 在安装时把这三样
// 装进 /app，运行期再往 home 写是多余的，且沙箱内 home 可能只读；Android 没有
// XDG 桌面这套机制。
//
// 每次启动都重写而不是"缺失才创建"：desktop 文件里的 Exec 必须写可执行文件的绝对
// 路径，而用户可能移动或重命名二进制，重写才能让菜单项跟着更新。代价是每次启动几
// 次小写入，可以忽略。
//
// 任何一步失败都只记日志。桌面集成是锦上添花，不该让应用起不来——用户直接运行二
// 进制时有没有菜单项完全不影响阅读功能。
func installDesktopIntegration() {
	dataHome := xdgDataHome()
	if dataHome == "" {
		slog.Debug("未找到 XDG 数据目录，跳过桌面集成")
		return
	}

	exePath, err := executablePath()
	if err != nil {
		slog.Debug("无法确定可执行文件路径，跳过桌面集成", "err", err)
		return
	}

	if err := writeDesktopFile(dataHome, exePath); err != nil {
		slog.Debug("写入桌面文件失败", "err", err)
	}
	if err := writeShareFile(dataHome, mimeFileName,
		filepath.Join("mime", "packages", mimeFileName)); err != nil {
		slog.Debug("写入 MIME 定义失败", "err", err)
	}
	if err := writeShareFile(dataHome, iconFileName,
		filepath.Join("icons", "hicolor", "scalable", "apps", iconFileName)); err != nil {
		slog.Debug("写入图标失败", "err", err)
	}

	// MIME 定义文件只是注册了类型，还得刷新 glob 数据库桌面环境和文件管理器才知道
	// .ofd 属于 application/ofd。命令来自 shared-mime-info 包，不是所有系统都装，
	// 找不到就跳过——文件已经写入，只是可能需要注销重登才生效。
	refreshMimeDatabase(dataHome)
}

// xdgDataHome 返回 XDG 数据目录。规范要求 $XDG_DATA_HOME 优先，未设时退回
// ~/.local/share；HOME 不可用时返回空串表示放弃（此时无法定位用户目录）。
func xdgDataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

// executablePath 返回可执行文件的绝对路径。解析符号链接，否则通过软链启动时
// Exec 会指向软链本身，菜单点击后找不到真实文件。
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// 软链目标不存在时保留原路径，总比没有 Exec 强。
		return exe, nil
	}
	return resolved, nil
}

// writeDesktopFile 写出 desktop 文件，并把 Exec 里的命令名替换成绝对路径。
//
// 内嵌文件里写的是 ofd-viewer（依赖 PATH），但用户多半是直接运行下载的二进制，
// PATH 里没有这个命令，菜单项点了会报"找不到命令"。%F 必须保留：它让文件管理器
// 把双击的文件名传给应用。
func writeDesktopFile(dataHome, exePath string) error {
	content, err := shareFiles.ReadFile("share/" + desktopFileName)
	if err != nil {
		return err
	}
	replaced, err := replaceExecCommand(content, exePath)
	if err != nil {
		return err
	}
	return writeUserFile(filepath.Join(dataHome, "applications", desktopFileName), replaced)
}

// replaceExecCommand 把 Exec 行的命令名换成绝对路径，保持原有的 %F 等字段。
func replaceExecCommand(content []byte, exePath string) ([]byte, error) {
	const key = "Exec="
	lines := bytes.Split(content, []byte{'\n'})
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
		// 只替换首个字段（命令本身），%F 之类的占位符保持原样。
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return nil, fmt.Errorf("Exec 行为空: %q", trimmed)
		}
		fields[0] = exePath
		lines[i] = []byte(key + strings.Join(fields, " "))
		found = true
		break
	}
	if !found {
		return nil, fmt.Errorf("desktop 文件缺少 %s 行", key)
	}
	return bytes.Join(lines, []byte{'\n'}), nil
}

// writeShareFile 把内嵌文件写到 XDG 数据目录下的指定子路径。
func writeShareFile(dataHome, name, rel string) error {
	content, err := shareFiles.ReadFile("share/" + name)
	if err != nil {
		return err
	}
	return writeUserFile(filepath.Join(dataHome, rel), content)
}

// writeUserFile 创建父目录并写文件。目录权限 0700、文件 0644：XDG 目录本身是
// 用户私有的，桌面环境读同用户的文件即可，不需要全局可读。
func writeUserFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// refreshMimeDatabase 重建 MIME 数据库。命令不存在时静默跳过——缺 shared-mime-info
// 的系统上 .ofd 的关联要靠桌面文件里的 MimeType，文件仍然写了。
func refreshMimeDatabase(dataHome string) {
	bin, err := exec.LookPath("update-mime-database")
	if err != nil {
		slog.Debug("未找到 update-mime-database，MIME 关联可能需要重新登录才生效")
		return
	}
	cmd := exec.Command(bin, filepath.Join(dataHome, "mime"))
	if output, err := cmd.CombinedOutput(); err != nil {
		slog.Debug("刷新 MIME 数据库失败", "err", err, "output", strings.TrimSpace(string(output)))
	}
}
