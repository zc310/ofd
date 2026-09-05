//go:build flatpak && !android

package main

import (
	"fmt"
	"net/url"
	"path/filepath"

	"fyne.io/fyne/v2"
	"github.com/rymdport/portal/filechooser"
)

func chooseOpenFile(title string, _ fyne.Window) (fileSelection, error) {
	uris, err := filechooser.OpenFile("", title, &filechooser.OpenFileOptions{
		AcceptLabel: "打开",
		Filters: []*filechooser.Filter{{
			Name: "OFD 文件",
			Rules: []filechooser.Rule{
				{Type: filechooser.GlobPattern, Pattern: "*.ofd"},
				{Type: filechooser.GlobPattern, Pattern: "*.OFD"},
			},
		}},
	})
	if err != nil {
		return fileSelection{}, err
	}
	path, err := portalURIPath(uris)
	if err != nil || path == "" {
		return fileSelection{}, err
	}
	return fileSelection{path: path, name: filepath.Base(path), input: path}, nil
}

func chooseSaveFile(title, fileName, extension string, _ fyne.Window) (fileSelection, error) {
	uris, err := filechooser.SaveFile("", title, &filechooser.SaveFileOptions{
		AcceptLabel: "保存",
		CurrentName: fileName,
		Filters: []*filechooser.Filter{{
			Name: "导出文件",
			Rules: []filechooser.Rule{
				{Type: filechooser.GlobPattern, Pattern: "*." + extension},
				{Type: filechooser.GlobPattern, Pattern: "*." + upperASCII(extension)},
			},
		}},
	})
	if err != nil {
		return fileSelection{}, err
	}
	path, err := portalURIPath(uris)
	if err != nil || path == "" {
		return fileSelection{}, err
	}
	return fileSelection{path: path, name: filepath.Base(path)}, nil
}

func portalURIPath(uris []string) (string, error) {
	if len(uris) == 0 {
		return "", nil
	}
	parsed, err := url.Parse(uris[0])
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "file" || parsed.Host != "" || parsed.Path == "" {
		return "", fmt.Errorf("文件选择器返回了非本地文件 URI: %s", uris[0])
	}
	return filepath.Clean(parsed.Path), nil
}

func upperASCII(value string) string {
	result := []byte(value)
	for i, char := range result {
		if char >= 'a' && char <= 'z' {
			result[i] = char - ('a' - 'A')
		}
	}
	return string(result)
}
