package media

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ContentReader 提供读取文件解压后内容的能力。
type ContentReader interface {
	ParseContent(string) ([]byte, error)
}

// Decode 从包内容读取器中读取并解码栅格图像。
func Decode(reader ContentReader, filename string) (image.Image, error) {
	if reader == nil {
		return nil, fmt.Errorf("图像内容读取器为空")
	}
	data, err := reader.ParseContent(filename)
	if err != nil {
		return nil, fmt.Errorf("读取图像失败: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// ExtractFirstImage 从基于 ZIP 的文件中提取第一张可以解码的图片。
func ExtractFirstImage(filename string) (image.Image, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	for _, entry := range archive.File {
		if !IsImageExtension(filepath.Ext(entry.Name)) {
			continue
		}
		if img, ok := decodeZipImage(entry); ok {
			return img, nil
		}
	}

	return nil, fmt.Errorf("未找到图片")
}

func decodeZipImage(entry *zip.File) (img image.Image, ok bool) {
	reader, err := entry.Open()
	if err != nil {
		return nil, false
	}
	defer reader.Close()

	img, _, err = image.Decode(reader)
	return img, err == nil
}

// IsImageExtension 判断扩展名是否为支持的栅格图片格式。
func IsImageExtension(ext string) bool {
	_, ok := imageExtensions[strings.ToLower(ext)]
	return ok
}

var imageExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".gif":  {},
	".bmp":  {},
	".webp": {},
	".tiff": {},
	".tif":  {},
}
