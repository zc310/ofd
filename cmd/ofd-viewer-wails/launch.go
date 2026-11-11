package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errLaunchOption = errors.New("不支持的启动选项")
var errLaunchFile = errors.New("请指定一个 OFD 文件")

// 启动路径只能来自命令行、系统打开事件或单实例转发，不接收网页提供的路径。
func launchPath(args []string, directory string) (string, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	} else if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		return "", errLaunchOption
	}
	if len(args) == 0 {
		return "", nil
	}
	if len(args) != 1 || !strings.EqualFold(filepath.Ext(args[0]), ".ofd") {
		return "", errLaunchFile
	}
	path := args[0]
	if !filepath.IsAbs(path) && directory != "" {
		path = filepath.Join(directory, path)
	}
	return filepath.Abs(path)
}

func validateLaunchFile(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".ofd") {
		return errLaunchFile
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读取启动文件: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errDocumentNotRegular
	}
	return nil
}

func (d *Desktop) queueLaunch(path string) {
	d.launchMu.Lock()
	defer d.launchMu.Unlock()
	// 有界队列，前端忙碌或尚未就绪时不丢失系统打开请求，也不提前替换当前会话。
	if len(d.launchPending) < 16 {
		d.launchPending = append(d.launchPending, path)
	} else {
		fmt.Fprintln(os.Stderr, "系统打开请求已达上限，请稍后重新打开文件")
	}
}

// PendingLaunch 仅探测是否有待处理的系统打开请求，不消费路径。
func (d *Desktop) PendingLaunch() bool {
	d.launchMu.Lock()
	defer d.launchMu.Unlock()
	return len(d.launchPending) != 0
}

// OpenLaunch 在前端取得文件选择锁后消费请求，不向网页暴露本机路径。
// 启动及系统打开复用解锁、权限、目录记录和最近文档流程。
func (d *Desktop) OpenLaunch() (*openedDocument, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	d.launchMu.Lock()
	if len(d.launchPending) == 0 {
		d.launchMu.Unlock()
		return nil, nil //nolint:nilnil // 没有待处理请求与取消文件对话框一致。
	}
	path := d.launchPending[0]
	d.launchPending = d.launchPending[1:]
	d.launchMu.Unlock()
	if err := validateLaunchFile(path); err != nil {
		return nil, err
	}
	return d.openChosenPath(path)
}
