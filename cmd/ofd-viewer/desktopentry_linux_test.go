//go:build linux && !flatpak && !android

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplaceExecCommand 保护 Exec 行的替换逻辑。
//
// 内嵌的 desktop 文件写的是 ofd-viewer，依赖 PATH 找命令；但用户多半是直接运行下载的
// 二进制，PATH 里没有它，菜单项点了会报"找不到命令"。所以必须换成绝对路径。
// 而 %F 这类占位符要原样保留——它是文件管理器把双击的文件名传给应用的唯一途径，
// 丢了就等于双击打不开文件。
func TestReplaceExecCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "替换命令名为绝对路径",
			in:   "[Desktop Entry]\nExec=ofd-viewer %F\n",
			want: "[Desktop Entry]\nExec=/opt/ofd/ofd-viewer %F\n",
		},
		{
			name: "无占位符也要替换",
			in:   "Exec=ofd-viewer\n",
			want: "Exec=/opt/ofd/ofd-viewer\n",
		},
		{
			name: "保留其他字段",
			in:   "Exec=ofd-viewer --flag %U\n",
			want: "Exec=/opt/ofd/ofd-viewer --flag %U\n",
		},
		{
			name: "容忍缩进与行尾空格",
			in:   "  Exec=  ofd-viewer   %F  \n",
			// 替换只重建首行内容，行尾的 \n 由 bytes.Join 保留。
			want: "Exec=/opt/ofd/ofd-viewer %F\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := replaceExecCommand([]byte(tc.in), "/opt/ofd/ofd-viewer")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("Exec 替换结果 = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

// TestReplaceExecCommandRejectsMissingExec 缺 Exec 行要报错而不是静默产出无法启动的
// desktop 文件——那种文件会被桌面环境正常索引，点了却什么都不发生。
func TestReplaceExecCommandRejectsMissingExec(t *testing.T) {
	for _, in := range []string{
		"[Desktop Entry]\nName=OFD Viewer\n",
		"[Desktop Entry]\nExec=\n",
	} {
		if _, err := replaceExecCommand([]byte(in), "/opt/ofd/ofd-viewer"); err == nil {
			t.Errorf("输入 %q 缺少可用的 Exec，应当报错", in)
		}
	}
}

// TestInstallDesktopIntegrationWritesAllFiles 验证三个文件都写到 XDG 数据目录下的正确
// 位置，且 Exec 指向可执行文件的绝对路径。
//
// 位置错了不会被发现：desktop 文件写到 ~/.config 或 mime 写到别处，文件都存在，
// 但桌面环境不索引，双击 .ofd 也不会关联到应用。
func TestInstallDesktopIntegrationWritesAllFiles(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	// refreshMimeDatabase 会调 update-mime-database，测试里指向不存在的命令，
	// 避免真的改系统 MIME 数据库。
	t.Setenv("PATH", t.TempDir())

	installDesktopIntegration()

	desktopPath := filepath.Join(dataHome, "applications", desktopFileName)
	desktop, err := os.ReadFile(desktopPath)
	if err != nil {
		t.Fatalf("未写出桌面文件: %v", err)
	}
	exe, err := executablePath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(desktop), "Exec="+exe) {
		t.Errorf("Exec 未指向可执行文件绝对路径 %q:\n%s", exe, desktop)
	}

	mimePath := filepath.Join(dataHome, "mime", "packages", mimeFileName)
	if _, err := os.Stat(mimePath); err != nil {
		t.Errorf("未写出 MIME 定义: %v", err)
	}

	iconPath := filepath.Join(dataHome, "icons", "hicolor", "scalable", "apps", iconFileName)
	if _, err := os.Stat(iconPath); err != nil {
		t.Errorf("未写出图标: %v", err)
	}
}

// TestXDGDataHome 覆盖目录定位：XDG_DATA_HOME 优先，其次 ~/.local/share，
// HOME 不可用时返回空串让调用方放弃而不是写到错误位置。
func TestXDGDataHome(t *testing.T) {
	t.Run("XDG_DATA_HOME 优先", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/xdg/data")
		t.Setenv("HOME", "/home/user")
		if got := xdgDataHome(); got != "/xdg/data" {
			t.Fatalf("xdgDataHome() = %q, 期望 /xdg/data", got)
		}
	})

	t.Run("未设时退回 HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("HOME", "/home/user")
		want := filepath.Join("/home/user", ".local", "share")
		if got := xdgDataHome(); got != want {
			t.Fatalf("xdgDataHome() = %q, 期望 %q", got, want)
		}
	})

	t.Run("相对路径按规范忽略", func(t *testing.T) {
		// XDG 规范要求 XDG_DATA_HOME 是绝对路径，相对值必须忽略。
		t.Setenv("XDG_DATA_HOME", "relative/path")
		t.Setenv("HOME", "/home/user")
		want := filepath.Join("/home/user", ".local", "share")
		if got := xdgDataHome(); got != want {
			t.Fatalf("xdgDataHome() = %q, 期望忽略相对值并用 %q", got, want)
		}
	})
}

// TestEmbeddedShareFiles 三个内嵌文件都要能读到，且是预期格式。
//
// embed 路径写错只会在运行时暴露：desktop 文件缺失会让应用菜单里没有条目，
// MIME 定义缺失会让双击打不开文件——两者都不会报错，只是功能悄悄没了。
func TestEmbeddedShareFiles(t *testing.T) {
	cases := []struct {
		name     string
		contains []string
	}{
		{
			// Icon= 按规范只写图标名不带扩展名，桌面环境自己去 hicolor 主题目录
			// 按名查找——所以这里期望 applicationID 而不是带 .svg 的文件名。
			name:     desktopFileName,
			contains: []string{"[Desktop Entry]", "Exec=", "MimeType=application/ofd;", "Icon=" + applicationID},
		},
		{
			name:     mimeFileName,
			contains: []string{"<mime-info", `type="application/ofd"`, `pattern="*.ofd"`},
		},
		{
			name:     iconFileName,
			contains: []string{"<svg", "viewBox"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, err := shareFiles.ReadFile("share/" + tc.name)
			if err != nil {
				t.Fatalf("读取内嵌文件失败: %v", err)
			}
			for _, want := range tc.contains {
				if !strings.Contains(string(content), want) {
					t.Errorf("%s 缺少 %q", tc.name, want)
				}
			}
		})
	}
}
