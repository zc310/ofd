package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 仅在本机保存目录，不保存文档内容，也不向网页暴露绝对路径。
func (d *Desktop) loadOpenDirectory() {
	directory, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法确定文件对话框配置目录: %v\n", err)
		return
	}
	d.openDirectoryFile = filepath.Join(directory, "ofd-viewer-wails", "open-directory")
	data, err := os.ReadFile(d.openDirectoryFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "读取文件对话框目录失败: %v\n", err)
	}
	if err == nil {
		d.openDirectory = string(data)
	}
}

// 对话框和记录更新串行执行；取消或对话框错误不会覆盖已有记录。
func (d *Desktop) chooseAndOpen(choose func(string) (string, error)) (*openedDocument, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	defer d.prunePlatformImports()
	directory := d.openDirectory
	if info, err := os.Stat(directory); !filepath.IsAbs(directory) || err != nil || !info.IsDir() {
		directory = ""
	}
	path, err := choose(directory)
	if err != nil || path == "" {
		return nil, err
	}
	return d.openChosenPath(path)
}

// 调用者持有 openMu；所有原生打开入口共用目录记录与最近文档更新。
func (d *Desktop) openChosenPath(path string) (*openedDocument, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析所选文件路径: %w", err)
	}
	d.openDirectory = filepath.Dir(absolute)
	if d.openDirectoryFile != "" {
		if err := saveOpenDirectory(d.openDirectoryFile, d.openDirectory); err != nil {
			// 配置不可写不妨碍打开文档，本次运行仍使用内存中的目录。
			fmt.Fprintf(os.Stderr, "保存文件对话框目录失败: %v\n", err)
		}
	}
	return d.openPath(absolute)
}

// 临时文件与目标同目录，避免中断写入损坏上次记录。
func saveOpenDirectory(path, directory string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".open-directory-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(directory)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
