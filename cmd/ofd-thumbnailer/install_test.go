package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
