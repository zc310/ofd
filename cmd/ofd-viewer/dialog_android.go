//go:build android

package main

import (
	"errors"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/x/explorer"
)

const maxAndroidDocumentSize = 128 << 20

type viewerPicker struct{ explorer *explorer.Explorer }

func newViewerPicker(window *app.Window) *viewerPicker {
	return &viewerPicker{explorer: explorer.NewExplorer(window)}
}

func (p *viewerPicker) listen(e event.Event) { p.explorer.ListenEvents(e) }

func (p *viewerPicker) choose() (selectedFile, error) {
	// OFD 没有可靠注册的 MIME 类型，显示所有文件以避免系统选择器隐藏文档。
	file, err := p.explorer.ChooseFile()
	if errors.Is(err, explorer.ErrUserDecline) {
		return selectedFile{}, nil
	}
	if err != nil {
		return selectedFile{}, err
	}
	defer file.Close()
	name := "Android文档.ofd"
	if named, ok := file.(interface{ Name() string }); ok && named.Name() != "" {
		name = named.Name()
	}
	return readSelectedFile(name, file, maxAndroidDocumentSize)
}

func chooseExportFile(_, _ string, _ int) (string, error) {
	return "", viewerError("Android 导出尚未适配")
}
