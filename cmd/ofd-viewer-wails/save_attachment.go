package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/zc310/ofd/internal/fileutil"
	readersession "github.com/zc310/ofd/internal/reader/session"
)

var errAttachmentMissing = errors.New("附件不存在")
var errMediaMissing = errors.New("资源不存在")
var errOverwriteInput = errors.New("不能覆盖当前输入文档或其链接")
var errSaveNotRegular = errors.New("保存目标必须是普通文件")

// SaveAttachment 由原生对话框选择保存路径，不接受网页提供的输出路径。
func (d *Desktop) SaveAttachment(ctx context.Context, prefix string, scope int, id string) (bool, error) {
	return d.saveAttachmentWithPicker(ctx, prefix, scope, id, d.savePicker(ctx, "附件另存为"))
}

func (d *Desktop) saveAttachmentWithPicker(ctx context.Context, prefix string, scope int, id string, choose func(string) (string, error)) (bool, error) {
	return d.saveResourceWithPicker(ctx, prefix, func() ([]byte, error) {
		return d.document.AttachmentData(ctx, prefix, scope, id)
	}, func() (string, error) {
		items, err := d.document.Attachments(ctx, prefix)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if item.Scope == scope && item.ID == id {
				return safeAttachmentFilename(item.Name), nil
			}
		}
		return "", errAttachmentMissing
	}, choose)
}

// SaveMedia 通过原生对话框保存资源原始内容，不执行或自动打开文件。
func (d *Desktop) SaveMedia(ctx context.Context, prefix string, scope int, id uint64) (bool, error) {
	return d.saveMediaWithPicker(ctx, prefix, scope, id, d.savePicker(ctx, "资源另存为"))
}

func (d *Desktop) saveMediaWithPicker(ctx context.Context, prefix string, scope int, id uint64, choose func(string) (string, error)) (bool, error) {
	return d.saveResourceWithPicker(ctx, prefix, func() ([]byte, error) {
		return d.document.MediaExportData(ctx, prefix, scope, id)
	}, func() (string, error) {
		items, err := d.document.Media(ctx, prefix)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if item.Scope == scope && item.ID == id {
				return safeAttachmentFilename(item.Name), nil
			}
		}
		return "", errMediaMissing
	}, choose)
}

// 附件与资源共享串行对话框、权限复核、取消和原子写入。
func (d *Desktop) saveResourceWithPicker(ctx context.Context, prefix string, read func() ([]byte, error), filename func() (string, error), choose func(string) (string, error)) (bool, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	if prefix != d.inputPrefix {
		return false, readersession.ErrStaleDocument
	}
	// 在显示保存对话框前检查导出权限；返回数据不传给网页。
	if _, err := read(); err != nil {
		return false, err
	}
	name, err := filename()
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := choose(name)
	defer releaseSaveTarget(path)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	// 对话框可能停留很久，实际写入前重新检查权限与会话。
	data, err := read()
	if err != nil {
		return false, err
	}
	if err := d.writeAttachment(ctx, path, data); err != nil {
		return false, err
	}
	return true, nil
}

func safeAttachmentFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) || strings.ContainsRune(`<>:"/\|?*`, char) {
			return '_'
		}
		return char
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "attachment"
	}
	return name
}

func (d *Desktop) checkAttachmentTarget(path string) error {
	if fileutil.SamePath(path, d.inputPath) {
		return errOverwriteInput
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.inputInfo != nil && os.SameFile(info, d.inputInfo) {
		return errOverwriteInput
	}
	if !info.Mode().IsRegular() {
		return errSaveNotRegular
	}
	return nil
}

// 使用同目录临时文件后替换，不截断输入文件，也不跟随目标文件符号链接写入。
func (d *Desktop) writeAttachment(ctx context.Context, path string, data []byte) error {
	if handled, err := writePlatformSave(ctx, path, data); handled {
		return err
	}
	if err := d.checkAttachmentTarget(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".ofd-attachment-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.checkAttachmentTarget(path); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
