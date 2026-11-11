package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errDropFiles = errors.New("请一次拖入一个 OFD 文件")
var errExpiredDrop = errors.New("拖入文件已失效，请重新拖入")
var errDocumentNotRegular = errors.New("所选文档必须是普通文件")

// 只接收原生窗口事件提供的路径；网页只能消费一次性标识。
func (d *Desktop) prepareDroppedFiles(paths []string) (string, error) {
	d.dropMu.Lock()
	defer d.dropMu.Unlock()
	d.dropToken, d.dropPath = "", ""
	if len(paths) != 1 || !strings.EqualFold(filepath.Ext(paths[0]), ".ofd") {
		return "", errDropFiles
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errDropFiles
	}
	d.dropToken, d.dropPath, d.dropExpires = randomDocumentToken(), paths[0], time.Now().Add(time.Minute)
	return d.dropToken, nil
}

// OpenDropped 仅打开刚刚从原生窗口拖入的文件，拒绝任意路径或重复消费。
func (d *Desktop) OpenDropped(token string) (*openedDocument, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	d.dropMu.Lock()
	if token == "" || token != d.dropToken || time.Now().After(d.dropExpires) {
		d.dropMu.Unlock()
		return nil, errExpiredDrop
	}
	path := d.dropPath
	d.dropToken, d.dropPath = "", ""
	d.dropMu.Unlock()
	return d.openChosenPath(path)
}
