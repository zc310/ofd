package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

const (
	maxRecentFiles  = 8
	maxSettingsSize = 128 << 10
)

var errSettingsTooLarge = errors.New("偏好文件过大")

type viewerSettings struct {
	Fit              string   `json:"fit"`
	Recent           []string `json:"recent"`
	Thumbnails       bool     `json:"thumbnails"`
	Continuous       bool     `json:"continuous"`
	DoublePage       bool     `json:"double_page"`
	HidePageControls bool     `json:"hide_page_controls"`
	Background       string   `json:"background"`
	BackgroundCustom string   `json:"background_custom"`
}

// loadSettings 不覆盖损坏的文件；调用者可选择在本次会话禁用持久化。
func loadSettings(path string) (viewerSettings, error) {
	settings := viewerSettings{Fit: "page"}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSettingsSize+1))
	if err != nil {
		return settings, err
	}
	if len(data) > maxSettingsSize {
		return settings, errSettingsTooLarge
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return viewerSettings{}, fmt.Errorf("解析偏好文件: %w", err)
	}
	if settings.Fit != "width" {
		settings.Fit = "page"
	}
	var recent []string
	for _, name := range settings.Recent {
		if name != "" && len(name) <= 4096 && !slices.Contains(recent, name) {
			recent = append(recent, name)
			if len(recent) == maxRecentFiles {
				break
			}
		}
	}
	settings.Recent = recent
	return settings, nil
}

// saveSettings 用同目录临时文件替换，避免中途失败破坏已有偏好。
func saveSettings(path string, settings viewerSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func rememberRecent(recent []string, filename string) []string {
	if filename == "" {
		return recent
	}
	absolute, err := filepath.Abs(filename)
	if err != nil || len(absolute) > 4096 {
		return recent
	}
	out := []string{absolute}
	for _, name := range recent {
		if name != absolute && len(out) < maxRecentFiles {
			out = append(out, name)
		}
	}
	return out
}
