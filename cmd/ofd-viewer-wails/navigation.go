package main

import (
	"context"
	"errors"
	"os"

	readercontract "github.com/zc310/ofd/internal/reader/contract"
	readersession "github.com/zc310/ofd/internal/reader/session"
)

var errVersionOptions = errors.New("版本参数无效")
var errVersionSourceChanged = errors.New("源文档已变化，请重新打开后切换版本")

// CustomTags 返回经过权限检查的自定义标签树。
func (d *Desktop) CustomTags(ctx context.Context, prefix string) ([]map[string]any, error) {
	tags, err := d.document.CustomTags(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return readercontract.CustomTags(tags), nil
}

// Versions 返回可用于切换的历史版本。
func (d *Desktop) Versions(ctx context.Context, prefix string) ([]map[string]any, error) {
	versions, err := d.document.Versions(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return readercontract.Versions(versions), nil
}

// SelectVersion 串行切换版本并轮换资源标识；原输入保护仍以原文件为准。
func (d *Desktop) SelectVersion(prefix string, scope int, id string) (readersession.OpenResult, error) {
	if scope < 0 || scope > 1<<20 || len(id) > 4096 {
		return readersession.OpenResult{}, errVersionOptions
	}
	d.openMu.Lock()
	defer d.openMu.Unlock()
	if d.unlockedView == nil && d.inputInfo != nil {
		info, err := os.Stat(d.inputPath)
		if err != nil {
			return readersession.OpenResult{}, err
		}
		if !os.SameFile(info, d.inputInfo) || info.Size() != d.inputInfo.Size() || !info.ModTime().Equal(d.inputInfo.ModTime()) {
			return readersession.OpenResult{}, errVersionSourceChanged
		}
	}
	result, err := d.document.SelectVersion(prefix, scope, id)
	if err == nil {
		d.inputPrefix = result.PagePrefix
		d.discardPrint()
	}
	return result, err
}
