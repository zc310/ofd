package main

import (
	"io"
	"path/filepath"
)

// readSelectedFile 将系统文件选择器返回的内容读入有界内存，不依赖可重开的文件路径。
func readSelectedFile(name string, file io.Reader, limit int64) (selectedFile, error) {
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return selectedFile{}, err
	}
	if int64(len(data)) > limit {
		return selectedFile{}, viewerError("文档超过内存读取大小限制")
	}
	return selectedFile{name: filepath.Base(name), data: data}, nil
}
