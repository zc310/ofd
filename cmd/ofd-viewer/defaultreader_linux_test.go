//go:build linux && !flatpak && !android

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubResult 描述被替换的外部命令的返回。
type stubResult struct {
	output string
	err    error
}

// stubDefaultReaderCommands 替换外部命令入口，返回记录到的调用。
//
// 探测和设置都只能通过这几个变量触达系统，用桩替换后测试既不会真的改用户配置，
// 也能覆盖命令缺失、输出异常这些分支。
func stubDefaultReaderCommands(t *testing.T, available map[string]bool, results map[string]stubResult) *[][2]string {
	t.Helper()
	calls := &[][2]string{}
	original := defaultReaderLookPath
	defaultReaderLookPath = func(name string) (string, error) {
		if !available[name] {
			return "", errors.New("命令不存在")
		}
		return "/stub/" + name, nil
	}
	t.Cleanup(func() { defaultReaderLookPath = original })

	run := func(name string) ([]byte, error) {
		result, ok := results[name]
		if !ok {
			return nil, errors.New("命令执行失败")
		}
		return []byte(result.output), result.err
	}
	originalOutput := defaultReaderOutput
	defaultReaderOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, [2]string{name, strings.Join(args, " ")})
		return run(name)
	}
	originalCombined := defaultReaderCombinedOutput
	defaultReaderCombinedOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, [2]string{name, strings.Join(args, " ")})
		return run(name)
	}
	t.Cleanup(func() {
		defaultReaderOutput = originalOutput
		defaultReaderCombinedOutput = originalCombined
	})
	return calls
}

// installStubDesktopFile 在 XDG 数据目录下放一个桌面文件，让探测认为应用已安装。
func installStubDesktopFile(t *testing.T) {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataHome, "applications"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataHome, "applications", desktopFileName), []byte("[Desktop Entry]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
}

// isolateMimeappsList 把 XDG 配置目录和 HOME 指到空目录，避免读到开发机上真实的
// 关联而让用例结果依赖环境。
func isolateMimeappsList(t *testing.T) string {
	t.Helper()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())
	return configHome
}

// TestMatchesDefaultReaderID 覆盖登记名的各种写法。
//
// 桌面环境返回的可能是 desktop ID、带后缀的桌面文件名或完整路径，大小写也不保证
// 一致。只按其中一种形式比较，会在真实系统上误判成"不是默认阅读器"并反复弹提示。
func TestMatchesDefaultReaderID(t *testing.T) {
	for _, id := range []string{
		applicationID,
		applicationID + ".desktop",
		"/home/user/.local/share/applications/" + applicationID + ".desktop",
		"/home/user/.local/share/applications/" + applicationID + ".DESKTOP",
		"  " + applicationID + ".desktop  ",
	} {
		if !matchesDefaultReaderID(id) {
			t.Errorf("%q 应当识别为本应用", id)
		}
	}
	for _, id := range []string{
		"",
		"   ",
		"org.example.other.desktop",
		applicationID + "-other.desktop",
		"ofd.desktop",
	} {
		if matchesDefaultReaderID(id) {
			t.Errorf("%q 不应识别为本应用", id)
		}
	}
}

// TestParseGioDefault 覆盖 gio 的输出格式。没有默认项时冒号后为空，必须返回空串
// 而不是把 "application/ofd" 当成应用名。
func TestParseGioDefault(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "标准输出",
			in:   "application/ofd: io.github.zc310.ofd.desktop\n",
			want: applicationID + ".desktop",
		},
		{name: "无默认项", in: "application/ofd: \n", want: ""},
		{name: "空输出", in: "", want: ""},
		{name: "多个候选取第一个", in: "application/ofd: a.desktop b.desktop", want: "a.desktop"},
		{
			name: "行首不是 MIME 类型时按解析失败处理",
			in:   "gio: 无法连接到 display\n",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseGioDefault(tc.in); got != tc.want {
				t.Errorf("parseGioDefault(%q) = %q, 期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseMimeappsDefault 覆盖配置文件的解析。
//
// 最关键的是不把 [Added Associations] 里的候选项当成默认项：那里放的是"曾经用过
// 的应用"，当成默认值会把用户的关联改错，而且这种错误只在真实环境里暴露。
func TestParseMimeappsDefault(t *testing.T) {
	content := strings.Join([]string{
		"# 注释行",
		"[Added Associations]",
		ofdMimeType + "=org.example.candidate.desktop;",
		"[Default Applications]",
		"text/plain=org.example.editor.desktop",
		ofdMimeType + "=" + applicationID + ".desktop;org.example.other.desktop",
		"[Removed Associations]",
		ofdMimeType + "=org.example.removed.desktop",
		"",
	}, "\n")
	id, ok := parseMimeappsDefault(content, ofdMimeType)
	if !ok {
		t.Fatal("应当解析出默认应用")
	}
	if id != applicationID+".desktop" {
		t.Errorf("默认应用 = %q, 期望 %q", id, applicationID+".desktop")
	}

	if _, ok := parseMimeappsDefault("[Added Associations]\n"+ofdMimeType+"=a.desktop;\n", ofdMimeType); ok {
		t.Error("[Added Associations] 里的条目不应被当作默认应用")
	}
	if _, ok := parseMimeappsDefault("", ofdMimeType); ok {
		t.Error("空内容不应解析出默认应用")
	}
	if _, ok := parseMimeappsDefault("[Default Applications]\n"+ofdMimeType+"=  ; ; \n", ofdMimeType); ok {
		t.Error("只有分隔符的取值不应被当作默认应用")
	}
}

// TestDetectDefaultReaderUsesXDGQuery 首选 xdg-mime 的结果，探测失败才逐级降级。
func TestDetectDefaultReaderUsesXDGQuery(t *testing.T) {
	installStubDesktopFile(t)
	isolateMimeappsList(t)

	t.Run("xdg-mime 报本应用为默认", func(t *testing.T) {
		calls := stubDefaultReaderCommands(t,
			map[string]bool{"xdg-mime": true, "gio": true},
			map[string]stubResult{"xdg-mime": {output: applicationID + ".desktop\n"}})
		if got := detectDefaultReader(); got != defaultReaderIsDefault {
			t.Errorf("detectDefaultReader() = %v, 期望 IsDefault", got)
		}
		if len(*calls) != 1 || (*calls)[0][0] != "xdg-mime" {
			t.Errorf("只应调用一次 xdg-mime，实际调用 %v", *calls)
		}
	})

	t.Run("xdg-mime 报别的应用", func(t *testing.T) {
		stubDefaultReaderCommands(t,
			map[string]bool{"xdg-mime": true, "gio": true},
			map[string]stubResult{"xdg-mime": {output: "org.example.other.desktop\n"}})
		if got := detectDefaultReader(); got != defaultReaderNotDefault {
			t.Errorf("detectDefaultReader() = %v, 期望 NotDefault", got)
		}
	})

	t.Run("查询成功但没有默认项", func(t *testing.T) {
		stubDefaultReaderCommands(t,
			map[string]bool{"xdg-mime": true},
			map[string]stubResult{"xdg-mime": {}})
		if got := detectDefaultReader(); got != defaultReaderNotDefault {
			t.Errorf("detectDefaultReader() = %v, 期望 NotDefault", got)
		}
	})

	t.Run("xdg-mime 缺失时用 gio", func(t *testing.T) {
		calls := stubDefaultReaderCommands(t,
			map[string]bool{"gio": true},
			map[string]stubResult{"gio": {output: "application/ofd: " + applicationID + ".desktop\n"}})
		if got := detectDefaultReader(); got != defaultReaderIsDefault {
			t.Errorf("detectDefaultReader() = %v, 期望 IsDefault", got)
		}
		if len(*calls) != 1 || (*calls)[0][0] != "gio" {
			t.Errorf("只应调用一次 gio，实际调用 %v", *calls)
		}
	})
}

// TestDetectDefaultReaderFallsBackToMimeappsList 两个命令都不可用时读配置文件。
//
// 少了这一层，缺 xdg-utils 的精简系统会永远停在 Unknown，用户既看不到提示也没法
// 从菜单里确认关联状态。
func TestDetectDefaultReaderFallsBackToMimeappsList(t *testing.T) {
	installStubDesktopFile(t)

	// 每个子用例都要独立的配置目录：残留的 mimeapps.list 会让"没有任何线索"
	// 读到上一个用例写下的关联而误判成 NotDefault。
	t.Run("配置文件里是本应用", func(t *testing.T) {
		configHome := isolateMimeappsList(t)
		writeMimeappsList(t, configHome, "[Default Applications]\n"+ofdMimeType+"="+applicationID+".desktop;\n")
		stubDefaultReaderCommands(t, map[string]bool{}, nil)
		if got := detectDefaultReader(); got != defaultReaderIsDefault {
			t.Errorf("detectDefaultReader() = %v, 期望 IsDefault", got)
		}
	})

	t.Run("配置文件里是别的应用", func(t *testing.T) {
		configHome := isolateMimeappsList(t)
		writeMimeappsList(t, configHome, "[Default Applications]\n"+ofdMimeType+"=org.example.other.desktop;\n")
		stubDefaultReaderCommands(t, map[string]bool{}, nil)
		if got := detectDefaultReader(); got != defaultReaderNotDefault {
			t.Errorf("detectDefaultReader() = %v, 期望 NotDefault", got)
		}
	})

	t.Run("没有任何线索", func(t *testing.T) {
		isolateMimeappsList(t)
		stubDefaultReaderCommands(t, map[string]bool{}, nil)
		if got := detectDefaultReader(); got != defaultReaderUnknown {
			t.Errorf("detectDefaultReader() = %v, 期望 Unknown", got)
		}
	})
}

// TestDetectDefaultReaderWithoutDesktopFile 桌面文件没装好时不做任何查询。
//
// 此时系统里没有以本应用为名的候选项，查询结果没有意义；继续查询还可能因为系统
// 里残留的旧登记而报出"不是默认"，让用户去点一个注定失败的提示。
func TestDetectDefaultReaderWithoutDesktopFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	isolateMimeappsList(t)

	calls := stubDefaultReaderCommands(t,
		map[string]bool{"xdg-mime": true},
		map[string]stubResult{"xdg-mime": {output: "org.example.other.desktop\n"}})
	if got := detectDefaultReader(); got != defaultReaderUnknown {
		t.Errorf("detectDefaultReader() = %v, 期望 Unknown", got)
	}
	if len(*calls) != 0 {
		t.Errorf("桌面文件缺失时不应执行外部命令，实际调用 %v", *calls)
	}
}

// TestSetDefaultReaderUsesXDGCommand 覆盖设置命令的选择与参数。
func TestSetDefaultReaderUsesXDGCommand(t *testing.T) {
	installStubDesktopFile(t)
	isolateMimeappsList(t)

	t.Run("优先 xdg-mime", func(t *testing.T) {
		calls := stubDefaultReaderCommands(t,
			map[string]bool{"xdg-mime": true, "gio": true},
			map[string]stubResult{"xdg-mime": {}})
		if err := setDefaultReader(); err != nil {
			t.Fatalf("setDefaultReader() 失败: %v", err)
		}
		want := [2]string{"xdg-mime", "default " + desktopFileName + " " + ofdMimeType}
		if len(*calls) != 1 || (*calls)[0] != want {
			t.Errorf("调用 = %v, 期望 [%v]", *calls, want)
		}
	})

	t.Run("退到 gio", func(t *testing.T) {
		calls := stubDefaultReaderCommands(t,
			map[string]bool{"gio": true},
			map[string]stubResult{"gio": {}})
		if err := setDefaultReader(); err != nil {
			t.Fatalf("setDefaultReader() 失败: %v", err)
		}
		want := [2]string{"gio", "mime " + ofdMimeType + " --default " + desktopFileName}
		if len(*calls) != 1 || (*calls)[0] != want {
			t.Errorf("调用 = %v, 期望 [%v]", *calls, want)
		}
	})
}

// TestSetDefaultReaderReportsFailure 失败必须带原因返回，不能静默成功。
//
// 静默成功会让菜单打上勾，用户随后双击 .ofd 打开的还是别的应用，而界面上一副
// 已经设好的样子。命令输出里通常写着具体原因，要带进错误信息。
func TestSetDefaultReaderReportsFailure(t *testing.T) {
	installStubDesktopFile(t)
	isolateMimeappsList(t)

	t.Run("命令失败时带出输出", func(t *testing.T) {
		stubDefaultReaderCommands(t,
			map[string]bool{"xdg-mime": true},
			map[string]stubResult{"xdg-mime": {output: "写入 mimeapps.list 失败：权限不足", err: errors.New("exit status 1")}})
		err := setDefaultReader()
		if err == nil {
			t.Fatal("命令失败时应当返回错误")
		}
		if !strings.Contains(err.Error(), "权限不足") {
			t.Errorf("错误信息应包含命令输出，实际为 %q", err)
		}
	})

	t.Run("命令都缺失", func(t *testing.T) {
		stubDefaultReaderCommands(t, map[string]bool{}, nil)
		err := setDefaultReader()
		if err == nil {
			t.Fatal("缺少命令时应当返回错误")
		}
		if !strings.Contains(err.Error(), "系统设置") {
			t.Errorf("错误信息应提示手动修改，实际为 %q", err)
		}
	})

	t.Run("桌面文件未安装", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		calls := stubDefaultReaderCommands(t,
			map[string]bool{"xdg-mime": true},
			map[string]stubResult{"xdg-mime": {}})
		if err := setDefaultReader(); err == nil {
			t.Fatal("桌面文件未安装时应当返回错误")
		}
		if len(*calls) != 0 {
			t.Errorf("桌面文件未安装时不应执行外部命令，实际调用 %v", *calls)
		}
	})
}

func writeMimeappsList(t *testing.T, configHome, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(configHome, "mimeapps.list"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
