package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/zc310/ofd/internal/fileutil"
)

const recentDocumentLimit = 5

var errUnknownRecentDocument = errors.New("最近文档记录不存在，请重新选择文件")

type recentDocument struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	LastOpened int64  `json:"last_opened"`
	Path       string `json:"path,omitempty"`
}

func randomDocumentToken() string {
	var token [32]byte
	_, _ = rand.Read(token[:])
	return hex.EncodeToString(token[:])
}

func (d *Desktop) loadRecentDocuments() {
	if d.openDirectoryFile == "" {
		return
	}
	d.recentFile = filepath.Join(filepath.Dir(d.openDirectoryFile), "recent-files.json")
	file, err := os.Open(d.recentFile)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取最近文档失败: %v\n", err)
		return
	}
	defer file.Close()
	var records []recentDocument
	if err := json.NewDecoder(io.LimitReader(file, 256<<10)).Decode(&records); err != nil {
		fmt.Fprintf(os.Stderr, "解析最近文档失败: %v\n", err)
		return
	}
	for _, record := range records {
		if !filepath.IsAbs(record.Path) || record.ID == "" {
			continue
		}
		d.recent = append(d.recent, record)
		if len(d.recent) == recentDocumentLimit {
			break
		}
	}
}

func (d *Desktop) persistRecentDocuments() error {
	if d.recentFile == "" {
		return nil
	}
	data, err := json.Marshal(d.recent)
	if err != nil {
		return err
	}
	return saveOpenDirectory(d.recentFile, string(data))
}

// openMu 保护本机路径清单；网页只得到不透明标识和显示元数据。
func (d *Desktop) rememberRecent(path string, info os.FileInfo) {
	defer d.prunePlatformImports()
	absolute, err := filepath.Abs(path)
	if err != nil {
		return
	}
	record := recentDocument{ID: randomDocumentToken(), Path: absolute, Name: filepath.Base(absolute), Size: info.Size(), LastOpened: time.Now().UnixMilli()}
	remaining := make([]recentDocument, 0, recentDocumentLimit)
	for _, old := range d.recent {
		if fileutil.SamePath(old.Path, absolute) {
			record.ID = old.ID
		} else {
			remaining = append(remaining, old)
		}
	}
	d.recent = append([]recentDocument{record}, remaining...)
	if len(d.recent) > recentDocumentLimit {
		d.recent = d.recent[:recentDocumentLimit]
	}
	if err := d.persistRecentDocuments(); err != nil {
		fmt.Fprintf(os.Stderr, "保存最近文档失败: %v\n", err)
	}
}

// RecentDocuments 返回最多五条最近文档，不传输本机路径或文件内容。
func (d *Desktop) RecentDocuments() []recentDocument {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	result := make([]recentDocument, len(d.recent))
	copy(result, d.recent)
	for index := range result {
		result[index].Path = ""
	}
	return result
}

// OpenRecent 仅能按本机已保存记录的标识打开文件，不接受前端路径。
func (d *Desktop) OpenRecent(id string) (*openedDocument, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	for _, record := range d.recent {
		if record.ID == id {
			return d.openChosenPath(record.Path)
		}
	}
	return nil, errUnknownRecentDocument
}

// ClearRecentDocuments 清除记录，不删除文档，也不关闭当前文档。
func (d *Desktop) ClearRecentDocuments() error {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	d.recent = nil
	return d.persistRecentDocuments()
}
