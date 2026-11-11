package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const desktopFileID = "io.github.zc310.ofd.wails.desktop"

var integrationCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) { //nolint:gochecknoglobals // 测试替身避免真实修改用户默认应用。
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

var errAssociationUnavailable = errors.New("default reader association unavailable")
var errAssociationUnconfirmed = errors.New("default reader association unconfirmed")

type readerAssociation struct {
	State    string `json:"state"`
	Settings bool   `json:"settings"`
}

func associationAvailable() bool {
	if runtime.GOOS != "linux" || os.Getenv("FLATPAK_ID") != "" {
		return false
	}
	dirs := filepath.SplitList(os.Getenv("XDG_DATA_DIRS"))
	if len(dirs) == 0 {
		dirs = []string{"/usr/local/share", "/usr/share"}
	}
	home := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(home) {
		if user, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(user, ".local", "share")
		}
	}
	for _, dir := range append([]string{home}, dirs...) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, "applications", desktopFileID)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// DefaultReader 只查询已安装的桌面关联；不把无法探测当作“不是默认”。
func (d *Desktop) DefaultReader(ctx context.Context) readerAssociation {
	if !associationAvailable() {
		return readerAssociation{State: "unavailable", Settings: runtime.GOOS == "windows" || runtime.GOOS == "darwin"}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := integrationCommand(ctx, "xdg-mime", "query", "default", "application/ofd")
	if err != nil {
		return readerAssociation{State: "unknown"}
	}
	if strings.TrimSpace(string(output)) == desktopFileID {
		return readerAssociation{State: "default"}
	}
	return readerAssociation{State: "other"}
}

// OpenDefaultReaderSettings 提供固定的系统设置入口或平台操作指引，不接收网页提供的命令。
// Windows/macOS 的默认应用选择由系统和用户确认，不伪造已设置成功的状态。
func (d *Desktop) OpenDefaultReaderSettings(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	switch runtime.GOOS {
	case "windows":
		_, err = integrationCommand(ctx, "explorer.exe", "ms-settings:defaultapps")
	case "darwin":
		d.app.Dialog.Info().SetTitle(d.tr("默认阅读器设置")).SetMessage(d.tr("在 Finder 中选择一个 OFD 文件，打开“显示简介”，在“打开方式”中选择 OFD Reader，然后点击“全部更改…”。")).Show()
		return nil
	default:
		return fmt.Errorf("%w: %s", errAssociationUnavailable, d.tr("此平台请在文件属性中选择默认阅读器"))
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	return err
}

// SetDefaultReader 仅由用户点击调用；安装包本身不抢占已有默认应用。
func (d *Desktop) SetDefaultReader(ctx context.Context) error {
	if !associationAvailable() {
		return fmt.Errorf("%w: %s", errAssociationUnavailable, d.tr("请先安装桌面文件；此平台请在系统设置中选择默认阅读器"))
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := integrationCommand(ctx, "xdg-mime", "default", desktopFileID, "application/ofd")
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	if err != nil {
		return fmt.Errorf("%s: %w: %s", d.tr("设置默认阅读器失败"), err, strings.TrimSpace(string(output)))
	}
	if state := d.DefaultReader(ctx); state.State != "default" {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return cancelErr
		}
		return fmt.Errorf("%w: %s", errAssociationUnconfirmed, d.tr("系统未确认默认阅读器设置，请在系统设置中检查"))
	}
	return nil
}
