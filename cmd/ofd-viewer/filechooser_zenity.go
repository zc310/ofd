//go:build !flatpak && !android

package main

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"github.com/ncruces/zenity"
)

func chooseOpenFile(title string, _ fyne.Window) (fileSelection, error) {
	path, err := zenity.SelectFile(
		zenity.Title(title),
		zenity.FileFilter{
			Name:     "OFD 文件",
			Patterns: []string{"*.ofd"},
			CaseFold: true,
		},
	)
	if err == zenity.ErrCanceled {
		return fileSelection{}, nil
	}
	return fileSelection{path: path, name: filepath.Base(path), input: path}, err
}

func chooseSaveFile(title, fileName, extension string, _ fyne.Window) (fileSelection, error) {
	path, err := zenity.SelectFileSave(
		zenity.Title(title),
		zenity.Filename(fileName),
		zenity.ConfirmOverwrite(),
		zenity.FileFilter{
			Name:     "导出文件",
			Patterns: []string{"*." + extension},
			CaseFold: true,
		},
	)
	if err == zenity.ErrCanceled {
		return fileSelection{}, nil
	}
	return fileSelection{path: path, name: filepath.Base(path)}, err
}
