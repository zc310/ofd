package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestSettingsRoundTripAndMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "settings.json")
	initial, err := loadSettings(path)
	if err != nil || initial.Fit != "page" || len(initial.Recent) != 0 || initial.HidePageControls {
		t.Fatalf("缺省偏好错误: %v %v", initial, err)
	}
	want := viewerSettings{Fit: "width", Recent: []string{filepath.Join(t.TempDir(), "文档.ofd")}, Thumbnails: true, Continuous: true, HidePageControls: true}
	if err := saveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil || got.Fit != want.Fit || got.Thumbnails != want.Thumbnails || got.Continuous != want.Continuous || got.HidePageControls != want.HidePageControls || !slices.Equal(got.Recent, want.Recent) {
		t.Fatalf("偏好往返错误: got=%v err=%v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("最近文件路径应仅用户可读: %v", info.Mode())
	}
}

func TestOldSettingsKeepPageControlsVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"fit":"width"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := loadSettings(path)
	if err != nil || settings.HidePageControls {
		t.Fatalf("旧偏好应默认显示页码栏: %v %v", settings, err)
	}
}

func TestRecentFilesDeduplicatedAndBounded(t *testing.T) {
	var recent []string
	for i := range 12 {
		recent = rememberRecent(recent, filepath.Join(t.TempDir(), strings.Repeat("x", i+1)+".ofd"))
	}
	if len(recent) != maxRecentFiles {
		t.Fatalf("最近文件数量 = %d", len(recent))
	}
	revisit := recent[5]
	recent = rememberRecent(recent, revisit)
	if len(recent) != maxRecentFiles || recent[0] != revisit || slices.Contains(recent[1:], revisit) {
		t.Fatalf("重复打开未提升到顶部: %v", recent)
	}
	if got := rememberRecent(recent, strings.Repeat("x", 5000)); !slices.Equal(got, recent) {
		t.Fatal("过长路径不应写入偏好")
	}
}

func TestCorruptAndOversizedSettingsAreNotOverwritten(t *testing.T) {
	for _, data := range []string{"{broken", strings.Repeat("x", maxSettingsSize+1)} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSettings(path); err == nil {
			t.Fatal("损坏或过大的偏好应返回错误")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatal("读取失败不应改写偏好")
		}
	}
}
