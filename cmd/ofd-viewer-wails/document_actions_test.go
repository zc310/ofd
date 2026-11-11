package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func desktopWithAttachment(t *testing.T, allowed bool) (*Desktop, *openedDocument, string) {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "native-actions", Pages: []creator.Page{{}},
		Permissions: &creator.Permissions{Export: &allowed},
		Attachments: []creator.Attachment{{ID: "a", Name: "../说明.txt", FileName: "note.txt", Data: []byte("附件内容")}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	desktop := &Desktop{document: service}
	opened, err := desktop.openPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return desktop, opened, path
}

func TestNativeRecentDocumentsPersistBoundAndHidePaths(t *testing.T) {
	desktop, _, input := desktopWithAttachment(t, true)
	desktop.recentFile = filepath.Join(t.TempDir(), "recent-files.json")
	firstID := desktop.RecentDocuments()[0].ID
	for index := range 7 {
		path := filepath.Join(t.TempDir(), strconv.Itoa(index)+".ofd")
		data, err := os.ReadFile(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := desktop.openChosenPath(path); err != nil {
			t.Fatal(err)
		}
	}
	list := desktop.RecentDocuments()
	if len(list) != 5 || list[0].Name != "6.ofd" {
		t.Fatalf("最近文档 = %+v", list)
	}
	encoded, err := json.Marshal(list)
	if err != nil || bytes.Contains(encoded, []byte(`"path"`)) || bytes.Contains(encoded, []byte(input)) {
		t.Fatalf("路径泄露: %s, %v", encoded, err)
	}
	if _, err := desktop.OpenRecent(firstID); !errors.Is(err, errUnknownRecentDocument) {
		t.Fatalf("过期记录 = %v", err)
	}
	list[0].Name = "修改后的副本"
	if desktop.RecentDocuments()[0].Name != "6.ofd" {
		t.Fatal("清单不是快照")
	}
	restored := &Desktop{document: desktop.document, openDirectoryFile: filepath.Join(filepath.Dir(desktop.recentFile), "open-directory")}
	restored.loadRecentDocuments()
	opened, err := restored.OpenRecent(list[0].ID)
	if err != nil || opened.Name != "6.ofd" || len(restored.RecentDocuments()) != 5 {
		t.Fatalf("恢复并重开 = %+v, %v", opened, err)
	}
	if err := restored.ClearRecentDocuments(); err != nil {
		t.Fatal(err)
	}
	if len(restored.RecentDocuments()) != 0 {
		t.Fatal("清除失败")
	}
	if _, err := restored.document.Page(opened.PagePrefix, 0); err != nil {
		t.Fatalf("清除不应关闭文档: %v", err)
	}
	if _, err := os.Stat(input); err != nil {
		t.Fatalf("清除不应删除文件: %v", err)
	}
}

func TestNativeRecentMissingFileKeepsCurrentDocument(t *testing.T) {
	desktop, opened, path := desktopWithAttachment(t, true)
	id := desktop.RecentDocuments()[0].ID
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.OpenRecent(id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缺失文件 = %v", err)
	}
	if _, err := desktop.document.Page(opened.PagePrefix, 0); err != nil {
		t.Fatalf("失败不应关闭当前文档: %v", err)
	}
}

func TestNativeDropAcceptsOnlyOneRegularOFDAndOneUseToken(t *testing.T) {
	desktop, _, path := desktopWithAttachment(t, true)
	for _, paths := range [][]string{nil, {path, path}, {filepath.Dir(path)}, {"other.txt"}} {
		if _, err := desktop.prepareDroppedFiles(paths); err == nil {
			t.Fatalf("接受了非法拖放: %v", paths)
		}
	}
	token, err := desktop.prepareDroppedFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.OpenDropped(path); !errors.Is(err, errExpiredDrop) {
		t.Fatalf("网页路径不应可打开: %v", err)
	}
	opened, err := desktop.OpenDropped(token)
	if err != nil || opened == nil {
		t.Fatalf("拖放打开 = %+v, %v", opened, err)
	}
	if _, err := desktop.OpenDropped(token); !errors.Is(err, errExpiredDrop) {
		t.Fatalf("标识重复消费 = %v", err)
	}
	token, err = desktop.prepareDroppedFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	desktop.dropExpires = time.Now().Add(-time.Second)
	if _, err := desktop.OpenDropped(token); !errors.Is(err, errExpiredDrop) {
		t.Fatalf("标识过期 = %v", err)
	}
}

func TestNativeSaveAttachmentPermissionAndCancellation(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(strconv.FormatBool(allowed), func(t *testing.T) {
			desktop, opened, _ := desktopWithAttachment(t, allowed)
			output := filepath.Join(t.TempDir(), "saved.txt")
			calls := 0
			saved, err := desktop.saveAttachmentWithPicker(t.Context(), opened.PagePrefix, 0, "a", func(name string) (string, error) {
				calls++
				if name != "说明.txt" {
					t.Errorf("默认文件名不安全: %q", name)
				}
				return output, nil
			})
			if !allowed {
				if !errors.Is(err, reader.ErrPermissionDenied) || saved || calls != 0 {
					t.Fatalf("权限拒绝 = %v, %v, %d", saved, err, calls)
				}
				return
			}
			if err != nil || !saved {
				t.Fatalf("保存 = %v, %v", saved, err)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "附件内容" {
				t.Fatalf("保存内容 = %q, %v", data, err)
			}
			if saved, err := desktop.saveAttachmentWithPicker(t.Context(), opened.PagePrefix, 0, "a", func(string) (string, error) { return "", nil }); err != nil || saved {
				t.Fatalf("取消 = %v, %v", saved, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if saved, err := desktop.saveAttachmentWithPicker(ctx, opened.PagePrefix, 0, "a", func(string) (string, error) { cancel(); return output, nil }); !errors.Is(err, context.Canceled) || saved {
				t.Fatalf("对话框期间取消 = %v, %v", saved, err)
			}
		})
	}
}

func TestNativeSaveAttachmentRejectsInputAndAliases(t *testing.T) {
	desktop, opened, input := desktopWithAttachment(t, true)
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{input}
	for _, link := range []struct {
		name   string
		create func(string, string) error
	}{{"symbolic.ofd", os.Symlink}, {"hard.ofd", os.Link}} {
		path := filepath.Join(t.TempDir(), link.name)
		if err := link.create(input, path); err != nil {
			t.Logf("当前环境无法创建链接: %v", err)
			continue
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if saved, err := desktop.saveAttachmentWithPicker(t.Context(), opened.PagePrefix, 0, "a", func(string) (string, error) { return path, nil }); !errors.Is(err, errOverwriteInput) || saved {
			t.Fatalf("覆盖输入保护 %s = %v, %v", path, saved, err)
		}
	}
	after, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("输入文档被修改")
	}
	if _, err := desktop.openChosenPath(input); err != nil {
		t.Fatal(err)
	}
	if saved, err := desktop.saveAttachmentWithPicker(t.Context(), opened.PagePrefix, 0, "a", func(string) (string, error) { t.Error("旧会话不应打开对话框"); return "", nil }); !errors.Is(err, readersession.ErrStaleDocument) || saved {
		t.Fatalf("旧会话保存 = %v, %v", saved, err)
	}
}

func TestNativeSaveAttachmentRechecksSessionAfterPicker(t *testing.T) {
	desktop, opened, input := desktopWithAttachment(t, true)
	output := filepath.Join(t.TempDir(), "not-written.txt")
	saved, err := desktop.saveAttachmentWithPicker(t.Context(), opened.PagePrefix, 0, "a", func(string) (string, error) {
		// 模拟对话框停留期间宿主会话已失效，不能使用先前读取的附件。
		_, err := desktop.document.Open(input, readersession.Options{})
		return output, err
	})
	if saved || !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("对话框后旧会话 = %v, %v", saved, err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("旧会话产生输出: %v", err)
	}
}

func TestAttachmentAtomicWritePreservesExistingFileWhenCancelled(t *testing.T) {
	desktop, _, _ := desktopWithAttachment(t, true)
	output := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(output, []byte("原有内容"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := desktop.writeAttachment(ctx, output, []byte("取消的内容")); !errors.Is(err, context.Canceled) {
		t.Fatalf("写入取消 = %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "原有内容" {
		t.Fatalf("取消破坏已有文件: %q, %v", data, err)
	}
	if err := desktop.writeAttachment(t.Context(), output, []byte("新的内容")); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(output)
	if err != nil || string(data) != "新的内容" {
		t.Fatalf("替换失败: %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatalf("残留临时文件: %v, %v", entries, err)
	}
}
