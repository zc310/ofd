package markdown

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// isRemote 判断图片地址是否为需要网络访问的远程地址。
func isRemote(source string) bool {
	lower := strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "ftp://") ||
		strings.HasPrefix(lower, "//")
}

// loadLocalImage 读取本地图片或 base64 data URI，保留无法读取时跳过图片的行为。
func loadLocalImage(source, baseDir string) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), "data:") {
		return decodeDataURI(source)
	}
	path := filepath.FromSlash(source)
	if !filepath.IsAbs(path) {
		if baseDir == "" {
			return nil, fmt.Errorf("%w：相对图片 %q 缺少基准目录", errMarkdownContent, source)
		}
		path = filepath.Join(baseDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return imageInfo(data)
}

func decodeDataURI(source string) ([]byte, error) {
	comma := strings.IndexByte(source, ',')
	if comma < 0 {
		return nil, fmt.Errorf("%w：data URI 缺少分隔符", errMarkdownContent)
	}
	header := source[:comma]
	if !strings.Contains(strings.ToLower(header), ";base64") {
		return nil, fmt.Errorf("%w：仅支持 base64 编码的 data URI，实际头 %q", errMarkdownContent, header)
	}
	data, err := base64.StdEncoding.DecodeString(source[comma+1:])
	if err != nil {
		return nil, err
	}
	return imageInfo(data)
}

// imageInfo 校验图片可解码且为 PNG/JPEG；具体尺寸交给 flow 测量。
func imageInfo(data []byte) ([]byte, error) {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(format) {
	case "png", "jpeg":
		return data, nil
	default:
		return nil, fmt.Errorf("%w：不支持的图片格式 %q", errMarkdownContent, format)
	}
}
