package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstallerStopsOnInstallFailure(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("installer tests require bash")
	}
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	// 临时副本只跳过 root 检查；全部 install 操作由不写系统目录的桩执行。
	const rootCheck = "if [[ $EUID -ne 0 ]]; then"
	if !strings.Contains(string(source), rootCheck) {
		t.Fatal("cannot locate installer root check")
	}
	script := strings.Replace(string(source), rootCheck, "if false; then", 1)
	for _, test := range []struct {
		failure string
		calls   int
	}{
		{"directory", 1},
		{"755", 2},
		{"644", 3},
		{"none", 3},
	} {
		t.Run(test.failure, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls.log")
			stub := `#!/bin/sh
printf '%s\n' "$*" >> "$THUMBNAILER_TEST_LOG"
if [ "$1" = '-d' ] && [ "$THUMBNAILER_TEST_FAILURE" = 'directory' ]; then exit 42; fi
if [ "$1" = '-m' ] && [ "$2" = "$THUMBNAILER_TEST_FAILURE" ]; then exit 42; fi
exit 0
`
			for name, content := range map[string]string{
				"install.sh": script, "install": stub,
				"ofd-thumbnailer": "test binary", "zc310-ofd.thumbnailer": "test configuration",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("bash", filepath.Join(dir, "install.sh"))
			command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"THUMBNAILER_TEST_LOG="+log, "THUMBNAILER_TEST_FAILURE="+test.failure)
			output, runErr := command.CombinedOutput()
			if test.failure == "none" {
				if runErr != nil || !strings.Contains(string(output), "安装完成") {
					t.Fatalf("successful install = %v, %s", runErr, output)
				}
			} else if runErr == nil || strings.Contains(string(output), "安装完成") {
				t.Fatalf("failed install reported success: error = %v, output = %s", runErr, output)
			}
			calls, err := os.ReadFile(log)
			if err != nil || strings.Count(string(calls), "\n") != test.calls {
				t.Fatalf("install calls = %q, error = %v; want %d calls", calls, err, test.calls)
			}
		})
	}
}

func TestInstallerBuildsSourceInTemporaryDirectoryBeforeInstalling(t *testing.T) {
	for _, test := range []struct {
		name         string
		cleanSource  bool
		buildFails   bool
		installFails bool
		elevate      bool
		wantInstalls int
	}{
		{name: "clean-source", cleanSource: true, wantInstalls: 3},
		{name: "success", wantInstalls: 3},
		{name: "build-failure", buildFails: true},
		{name: "install-failure", installFails: true, wantInstalls: 2},
		{name: "sudo-only-for-install", elevate: true, wantInstalls: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := exec.LookPath("bash"); err != nil {
				t.Skip("安装脚本测试需要 bash")
			}
			source, err := os.ReadFile("install.sh")
			if err != nil {
				t.Fatal(err)
			}
			const rootCheck = "if [[ $EUID -ne 0 ]]; then"
			if !strings.Contains(string(source), rootCheck) {
				t.Fatal("找不到安装权限检查")
			}
			condition := "if false; then"
			if test.elevate {
				condition = "if true; then"
			}
			// 仅模拟权限判断；go、install 和 sudo 均由桩执行，不修改系统文件。
			script := strings.Replace(string(source), rootCheck, condition, 1)
			dir := filepath.Join(t.TempDir(), "source with spaces")
			commandDir := filepath.Join(dir, "cmd", "ofd-thumbnailer")
			tempDir := filepath.Join(dir, "temporary builds")
			stubDir := filepath.Join(dir, "tools")
			for _, path := range []string{commandDir, tempDir, stubDir} {
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			goStub := `#!/bin/sh
set -eu
printf '%s\n' build >> "$THUMBNAILER_TEST_EVENTS"
[ "$CGO_ENABLED" = '0' ]
[ "$1" = '-C' ]
[ -f "$2/go.mod" ]
[ "$3" = 'build' ]
[ "$4" = '-trimpath' ]
[ "$5" = '-o' ]
[ "$7" = './cmd/ofd-thumbnailer' ]
printf '%s\n' "$6" > "$THUMBNAILER_TEST_BUILD_PATH"
printf '%s' 'compiled binary' > "$6"
if [ "$THUMBNAILER_TEST_BUILD_FAILS" = 'true' ]; then exit 41; fi
`
			installStub := `#!/bin/sh
set -eu
printf '%s\n' "install $1 ${2:-}" >> "$THUMBNAILER_TEST_EVENTS"
if [ "$1" = '-m' ] && [ "$2" = '755' ]; then
    [ "$(cat "$3")" = 'compiled binary' ]
    [ "$4" = '/usr/bin/zc310-ofd-thumbnailer' ]
    if [ "$THUMBNAILER_TEST_INSTALL_FAILS" = 'true' ]; then exit 42; fi
fi
if [ "$1" = '-m' ] && [ "$2" = '644' ]; then
    [ "$(cat "$3")" = 'test configuration' ]
    [ "$4" = '/usr/share/thumbnailers/zc310-ofd.thumbnailer' ]
fi
`
			sudoStub := `#!/bin/sh
set -eu
[ "$1" = 'install' ]
printf '%s\n' sudo >> "$THUMBNAILER_TEST_EVENTS"
exec "$@"
`
			for path, content := range map[string]string{
				filepath.Join(dir, "go.mod"):                       "module installer-test\n",
				filepath.Join(commandDir, "main.go"):               "package main\n",
				filepath.Join(commandDir, "install.sh"):            script,
				filepath.Join(commandDir, "ofd-thumbnailer"):       "stale binary",
				filepath.Join(commandDir, "zc310-ofd.thumbnailer"): "test configuration",
				filepath.Join(stubDir, "go"):                       goStub,
				filepath.Join(stubDir, "install"):                  installStub,
				filepath.Join(stubDir, "sudo"):                     sudoStub,
			} {
				if err := os.WriteFile(path, []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if test.cleanSource {
				if err := os.Remove(filepath.Join(commandDir, "ofd-thumbnailer")); err != nil {
					t.Fatal(err)
				}
			}
			eventsPath := filepath.Join(dir, "events.log")
			buildPathLog := filepath.Join(dir, "build-path.log")
			command := exec.Command("bash", filepath.Join(commandDir, "install.sh"))
			command.Dir = stubDir // 从其它目录启动也能找到模块及配置文件。
			command.Env = append(os.Environ(),
				"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TMPDIR="+tempDir,
				"THUMBNAILER_TEST_EVENTS="+eventsPath,
				"THUMBNAILER_TEST_BUILD_PATH="+buildPathLog,
				"THUMBNAILER_TEST_BUILD_FAILS="+strconv.FormatBool(test.buildFails),
				"THUMBNAILER_TEST_INSTALL_FAILS="+strconv.FormatBool(test.installFails))
			output, runErr := command.CombinedOutput()
			if test.buildFails || test.installFails {
				if runErr == nil || strings.Contains(string(output), "安装完成") {
					t.Fatalf("失败后仍继续安装: %v, %s", runErr, output)
				}
			} else if runErr != nil || !strings.Contains(string(output), "安装完成") {
				t.Fatalf("安装失败: %v, %s", runErr, output)
			}
			events, err := os.ReadFile(eventsPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(events), "build\n") || strings.Count(string(events), "install ") != test.wantInstalls {
				t.Fatalf("编译与安装顺序错误: %s", events)
			}
			wantSudo := 0
			if test.elevate {
				wantSudo = test.wantInstalls
			}
			if strings.Count(string(events), "sudo\n") != wantSudo {
				t.Fatalf("编译阶段提权或安装未正确提权: %s", events)
			}
			buildPath, err := os.ReadFile(buildPathLog)
			if err != nil || !strings.HasPrefix(string(buildPath), tempDir+string(os.PathSeparator)) {
				t.Fatalf("未编译到临时目录: %q, %v", buildPath, err)
			}
			remaining, err := os.ReadDir(tempDir)
			if err != nil || len(remaining) != 0 {
				t.Fatalf("退出后没有清理临时目录: %v, %v", remaining, err)
			}
			stale, err := os.ReadFile(filepath.Join(commandDir, "ofd-thumbnailer"))
			if test.cleanSource {
				if !os.IsNotExist(err) {
					t.Fatalf("编译在源码目录中生成了二进制: %v", err)
				}
			} else if err != nil || string(stale) != "stale binary" {
				t.Fatalf("编译覆盖了源码目录中的二进制: %q, %v", stale, err)
			}
		})
	}
}
