//go:build !js && !android

package main

import (
	"errors"
	"path/filepath"
	"strings"

	"gioui.org/app"
	"gioui.org/io/event"
	"github.com/ncruces/zenity"
)

type viewerPicker struct{}

func newViewerPicker(_ *app.Window) *viewerPicker { return &viewerPicker{} }
func (*viewerPicker) listen(_ event.Event)        {}
func (*viewerPicker) choose() (selectedFile, error) {
	name, err := chooseFile()
	return selectedFile{name: name}, err
}

func chooseFile() (string, error) {
	filename, err := zenity.SelectFile(zenity.Title("打开 OFD 文件"),
		zenity.FileFilter{Name: "OFD", Patterns: []string{"*.ofd"}, CaseFold: true})
	if errors.Is(err, zenity.ErrCanceled) {
		return "", nil
	}
	return filename, err
}

func chooseExportFile(source, format string, pages int) (string, error) {
	ext := exportExtension(format, pages)
	name := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)) + "." + ext
	path, err := zenity.SelectFileSave(zenity.Title("导出 OFD"), zenity.Filename(filepath.Join(filepath.Dir(source), name)), zenity.ConfirmOverwrite())
	if errors.Is(err, zenity.ErrCanceled) {
		return "", nil
	}
	return path, err
}
