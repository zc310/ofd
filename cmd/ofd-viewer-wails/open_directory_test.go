package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
)

func TestOpenDialogRemembersSelectedDirectoryAcrossRestarts(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	t.Setenv("HOME", config)
	data, err := creator.Marshal(creator.Document{ID: "dialog-directory", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "测试.ofd")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	desktop := &Desktop{document: service}
	desktop.loadOpenDirectory()
	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if desktop.openDirectoryFile != filepath.Join(userConfig, "ofd-viewer-wails", "open-directory") {
		t.Fatalf("打开目录配置路径未使用程序名: %q", desktop.openDirectoryFile)
	}
	result, err := desktop.chooseAndOpen(func(directory string) (string, error) {
		if directory != "" {
			t.Fatalf("首次起始目录 = %q", directory)
		}
		return path, nil
	})
	if err != nil || result == nil {
		t.Fatalf("选择文件 = %+v, %v", result, err)
	}
	for _, current := range []*Desktop{desktop, {document: service}} {
		if current != desktop {
			current.loadOpenDirectory()
		}
		for _, chooseErr := range []error{nil, os.ErrPermission} {
			result, err := current.chooseAndOpen(func(directory string) (string, error) {
				if directory != filepath.Dir(path) {
					t.Fatalf("未恢复选择目录: %q", directory)
				}
				return "", chooseErr
			})
			if result != nil || !errors.Is(err, chooseErr) || current.openDirectory != filepath.Dir(path) {
				t.Fatalf("取消或错误改变了目录: %+v, %v, %q", result, err, current.openDirectory)
			}
		}
	}
}

func TestOpenDialogFallsBackForInvalidSavedDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"", "relative", filepath.Join(t.TempDir(), "missing"), file} {
		desktop := &Desktop{openDirectory: directory}
		if _, err := desktop.chooseAndOpen(func(start string) (string, error) {
			if start != "" {
				t.Errorf("无效目录没有回退: %q", start)
			}
			return "", nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenDialogRemembersDirectoryEvenWhenDocumentIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.ofd")
	if err := os.WriteFile(path, []byte("不是 OFD"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	record := filepath.Join(t.TempDir(), "config", "open-directory")
	desktop := &Desktop{document: service, openDirectoryFile: record}
	if _, err := desktop.chooseAndOpen(func(string) (string, error) { return path, nil }); err == nil {
		t.Fatal("无效文档应返回错误")
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != filepath.Dir(path) {
		t.Fatalf("所选目录未保存: %q, %v", data, err)
	}
	other := t.TempDir()
	if err := saveOpenDirectory(record, other); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(record)
	if err != nil || string(data) != other {
		t.Fatalf("目录记录未更新: %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(record))
	if err != nil || len(entries) != 1 {
		t.Fatalf("临时文件未清理: %v, %v", entries, err)
	}
}
