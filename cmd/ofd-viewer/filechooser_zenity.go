//go:build !flatpak

package main

import "github.com/ncruces/zenity"

func chooseOpenFile(title string) (string, error) {
	path, err := zenity.SelectFile(
		zenity.Title(title),
		zenity.FileFilter{
			Name:     "OFD 文件",
			Patterns: []string{"*.ofd"},
			CaseFold: true,
		},
	)
	if err == zenity.ErrCanceled {
		return "", nil
	}
	return path, err
}

func chooseSaveFile(title, fileName, extension string) (string, error) {
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
		return "", nil
	}
	return path, err
}
