//go:build linux && !flatpak && !android

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ofdMimeType 是 .ofd 文件的 MIME 类型，由内嵌的 MIME 定义注册。
const ofdMimeType = "application/ofd"

// defaultReaderTimeout 限制外部命令的等待时间。探测在启动路径上，卡住的命令会
// 直接拖住窗口出现。
const defaultReaderTimeout = 2 * time.Second

// 外部命令都经由这三个变量调用，测试用桩替换，避免真的改动系统文件关联。
var (
	defaultReaderLookPath       = exec.LookPath
	defaultReaderOutput         = runDefaultReaderCommandOutput
	defaultReaderCombinedOutput = runDefaultReaderCommandCombined
)

func runDefaultReaderCommandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func runDefaultReaderCommandCombined(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// detectDefaultReader 探测 application/ofd 的默认阅读器。
//
// 依次尝试 xdg-mime、gio 和 mimeapps.list。前两个是标准查询入口，第三个是最后的
// 手段：只能反映用户手写下的关联，不反映桌面环境缓存的判定，但总比完全不知道好。
func detectDefaultReader() defaultReaderState {
	// 桌面文件没装好时任何查询都不作数：系统里根本没有可被选中的候选项。
	if !desktopFileInstalled() {
		return defaultReaderUnknown
	}
	if output, err := queryDefaultReader("xdg-mime", "query", "default", ofdMimeType); err != nil {
		slog.Debug("xdg-mime 查询失败", "err", err)
	} else {
		return defaultReaderStateFromID(string(output))
	}
	if output, err := queryDefaultReader("gio", "mime", ofdMimeType); err != nil {
		slog.Debug("gio 查询失败", "err", err)
	} else {
		return defaultReaderStateFromID(parseGioDefault(string(output)))
	}
	if id, ok := mimeappsDefault(ofdMimeType); ok {
		return defaultReaderStateFromID(id)
	}
	return defaultReaderUnknown
}

// defaultReaderStateFromID 把查询结果转成状态。查询成功但结果为空说明没有任何
// 默认项，同样属于"不是本应用"。
func defaultReaderStateFromID(id string) defaultReaderState {
	if matchesDefaultReaderID(id) {
		return defaultReaderIsDefault
	}
	return defaultReaderNotDefault
}

// matchesDefaultReaderID 判断查询结果是否指向本应用。
//
// 桌面环境登记的名字可能是 desktop ID、带 .desktop 后缀的桌面文件名，甚至完整
// 路径，大小写也不保证一致，因此不能直接比较字符串。
func matchesDefaultReaderID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if separator := strings.LastIndexAny(id, `/\`); separator >= 0 {
		id = id[separator+1:]
	}
	id = strings.TrimSuffix(strings.ToLower(id), ".desktop")
	return id == applicationID
}

// parseGioDefault 从 `gio mime application/ofd` 的输出里取默认阅读器。输出形如
// "application/ofd: io.github.zc310.ofd.desktop"，没有默认项时冒号后为空。
//
// 行首必须是 MIME 类型本身，否则一律当作解析失败：gio 偶尔会把错误信息写进标准
// 输出，那时把整行当答案会得到一个编造出来的应用名。
func parseGioDefault(output string) string {
	name, value, found := strings.Cut(strings.TrimSpace(output), ":")
	if !found || !strings.EqualFold(strings.TrimSpace(name), ofdMimeType) {
		return ""
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// mimeappsDefault 依次读 XDG 配置文件里的 mimeapps.list，返回某个 MIME 类型的
// 默认应用。
func mimeappsDefault(mimeType string) (string, bool) {
	seen := make(map[string]bool)
	for _, path := range mimeappsListPaths() {
		if seen[path] {
			continue
		}
		seen[path] = true
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if id, ok := parseMimeappsDefault(string(content), mimeType); ok {
			return id, true
		}
	}
	return "", false
}

// mimeappsListPaths 返回可能存放 mimeapps.list 的路径。规范要求 XDG_CONFIG_HOME
// 优先，未设时退回 ~/.config，两者可能指向同一处，调用方需要去重。
func mimeappsListPaths() []string {
	paths := make([]string, 0, 2)
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		paths = append(paths, filepath.Join(dir, "mimeapps.list"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "mimeapps.list"))
	}
	return paths
}

// parseMimeappsDefault 从 mimeapps.list 内容里取某个 MIME 类型的默认应用。
//
// 只认 [Default Applications] 段：[Added Associations] 里的条目是"曾经用过的
// 应用"，把它当默认项会把用户的关联改错。其余段（[Removed Associations] 和桌面
// 环境私有的段）同样不参与。
func parseMimeappsDefault(content, mimeType string) (string, bool) {
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section != "default applications" {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != mimeType {
			continue
		}
		// 取值是分号分隔的候选项列表，第一项才是默认项。
		for _, entry := range strings.Split(value, ";") {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				return entry, true
			}
		}
	}
	return "", false
}

// setDefaultReader 把本应用登记为 application/ofd 的默认阅读器。
//
// 交给 xdg-mime 或 gio 写 mimeapps.list，而不是自己拼一个最小化的文件：那些命令
// 会保留用户已有的分组和格式，应用自己写会把用户其它关联冲掉。
func setDefaultReader() error {
	if !desktopFileInstalled() {
		return errors.New("桌面文件尚未安装，无法登记为默认阅读器")
	}
	if _, err := defaultReaderLookPath("xdg-mime"); err == nil {
		return runDefaultReaderSetter("xdg-mime", "default", desktopFileName, ofdMimeType)
	}
	if _, err := defaultReaderLookPath("gio"); err == nil {
		return runDefaultReaderSetter("gio", "mime", ofdMimeType, "--default", desktopFileName)
	}
	return errors.New("系统缺少 xdg-mime 和 gio，无法自动设置默认阅读器，请在系统设置中手动修改")
}

// desktopFileInstalled 判断桌面文件是否已落到 XDG 数据目录。缺了它，系统里就没有
// 以本应用为名的候选项，登记默认阅读器必然失败。
func desktopFileInstalled() bool {
	dataHome := xdgDataHome()
	if dataHome == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dataHome, "applications", desktopFileName))
	return err == nil && !info.IsDir()
}

// queryDefaultReader 用指定命令查询默认阅读器，返回命令的标准输出。
func queryDefaultReader(name string, args ...string) (string, error) {
	if _, err := defaultReaderLookPath(name); err != nil {
		return "", fmt.Errorf("%s 不可用: %w", name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultReaderTimeout)
	defer cancel()
	output, err := defaultReaderOutput(ctx, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s 查询失败: %w", name, err)
	}
	return string(output), nil
}

// runDefaultReaderSetter 执行设置命令。失败时把命令输出带进错误信息：这些命令在
// 权限不足或配置目录不可写时会给出具体原因，光说"设置失败"用户无从下手。
func runDefaultReaderSetter(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultReaderTimeout)
	defer cancel()
	output, err := defaultReaderCombinedOutput(ctx, name, args...)
	if err == nil {
		return nil
	}
	if message := strings.TrimSpace(string(output)); message != "" {
		return fmt.Errorf("%s 设置默认阅读器失败: %w: %s", name, err, message)
	}
	return fmt.Errorf("%s 设置默认阅读器失败: %w", name, err)
}
