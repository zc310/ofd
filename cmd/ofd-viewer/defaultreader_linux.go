//go:build linux && !android && !flatpak

package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed share/io.github.zc310.ofd.desktop share/io.github.zc310.ofd.xml share/io.github.zc310.ofd.svg
var desktopResources embed.FS

const (
	defaultReaderDesktopID = "io.github.zc310.ofd.desktop"
	defaultReaderMIME      = "application/ofd"
)

type defaultReaderCommands struct {
	lookPath func(string) (string, error)
	run      func(context.Context, string, ...string) ([]byte, error)
}

func systemDefaultReaderCommands() defaultReaderCommands {
	return defaultReaderCommands{
		lookPath: exec.LookPath,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

func defaultReaderSupported() bool { return true }

func detectDefaultReader(ctx context.Context) defaultReaderState {
	state := queryDefaultReader(ctx, systemDefaultReaderCommands())
	if state != defaultReaderIsDefault {
		return state
	}
	exe, err := currentExecutable()
	if err != nil {
		return defaultReaderUnknown
	}
	dataHome, err := defaultReaderDataHome()
	if err != nil {
		return defaultReaderUnknown
	}
	data, err := os.ReadFile(filepath.Join(dataHome, "applications", defaultReaderDesktopID))
	if err != nil {
		return defaultReaderUnknown
	}
	expected := "Exec=" + desktopExecQuote(exe) + " %f"
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == expected {
			return defaultReaderIsDefault
		}
	}
	return defaultReaderNotDefault
}

func queryDefaultReader(ctx context.Context, commands defaultReaderCommands) defaultReaderState {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for _, query := range []struct {
		name string
		args []string
	}{
		{"xdg-mime", []string{"query", "default", defaultReaderMIME}},
		{"gio", []string{"mime", defaultReaderMIME}},
	} {
		if _, err := commands.lookPath(query.name); err != nil {
			continue
		}
		output, err := commands.run(ctx, query.name, query.args...)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(output))
		if query.name == "gio" {
			key, value, ok := strings.Cut(id, ":")
			if !ok || strings.TrimSpace(key) != defaultReaderMIME {
				continue
			}
			id, _, _ = strings.Cut(strings.TrimSpace(value), "\n")
		}
		if strings.EqualFold(filepath.Base(strings.TrimSpace(id)), defaultReaderDesktopID) {
			return defaultReaderIsDefault
		}
		return defaultReaderNotDefault
	}
	return defaultReaderUnknown
}

func setDefaultReader(ctx context.Context) error {
	exe, err := currentExecutable()
	if err != nil {
		return err
	}
	dataHome, err := defaultReaderDataHome()
	if err != nil {
		return err
	}
	return installDefaultReader(ctx, dataHome, exe, systemDefaultReaderCommands())
}

func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func defaultReaderDataHome() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataHome) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", viewerError("无法确定用户数据目录")
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return dataHome, nil
}

// desktopExecQuote 按 desktop entry 的双引号和字段代码规则保护程序路径。
func desktopExecQuote(path string) string {
	replacer := strings.NewReplacer("\\", "\\\\", `"`, `\"`, "`", "\\`", "$", "\\$", "%", "%%")
	return `"` + replacer.Replace(path) + `"`
}

func installDefaultReader(ctx context.Context, dataHome, exe string, commands defaultReaderCommands) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	desktop, err := desktopResources.ReadFile("share/io.github.zc310.ofd.desktop")
	if err != nil {
		return err
	}
	mime, err := desktopResources.ReadFile("share/io.github.zc310.ofd.xml")
	if err != nil {
		return err
	}
	icon, err := desktopResources.ReadFile("share/io.github.zc310.ofd.svg")
	if err != nil {
		return err
	}
	desktopText := strings.Replace(string(desktop), "Exec=ofd-viewer %F", "Exec="+desktopExecQuote(exe)+" %f", 1)
	if desktopText == string(desktop) {
		return viewerError("桌面资源缺少预期的 Exec 行")
	}
	files := []struct {
		path string
		data []byte
	}{
		{filepath.Join(dataHome, "applications", defaultReaderDesktopID), []byte(desktopText)},
		{filepath.Join(dataHome, "mime", "packages", "io.github.zc310.ofd.xml"), mime},
		{filepath.Join(dataHome, "icons", "hicolor", "scalable", "apps", "io.github.zc310.ofd.svg"), icon},
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(file.path, file.data, 0o644); err != nil {
			return err
		}
	}
	if _, err := commands.lookPath("update-mime-database"); err == nil {
		if output, err := commands.run(ctx, "update-mime-database", filepath.Join(dataHome, "mime")); err != nil {
			return fmt.Errorf("更新 MIME 数据库失败: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	for _, setter := range []struct {
		name string
		args []string
	}{
		{"xdg-mime", []string{"default", defaultReaderDesktopID, defaultReaderMIME}},
		{"gio", []string{"mime", defaultReaderMIME, "--default", defaultReaderDesktopID}},
	} {
		if _, err := commands.lookPath(setter.name); err != nil {
			continue
		}
		if output, err := commands.run(ctx, setter.name, setter.args...); err != nil {
			return fmt.Errorf("设置默认阅读器失败: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	return viewerError("系统缺少 xdg-mime 和 gio，无法设置默认阅读器")
}
