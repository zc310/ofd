//go:build flatpak

package main

import (
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/rymdport/portal/filechooser"
)

func chooseOpenFile(title string) (string, error) {
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
		return "", err
	}
	return portalURIPath(uris)
}

func chooseSaveFile(title, fileName, extension string) (string, error) {
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
		return "", err
	}
	return portalURIPath(uris)
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
