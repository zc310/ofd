package mdimport

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

// loadLocalImage 读取本地图片或 base64 data URI，并返回可用于 OFD 的格式与像素尺寸。
func loadLocalImage(source, baseDir string) ([]byte, string, int, int, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), "data:") {
		return decodeDataURI(source)
	}
	path := filepath.FromSlash(source)
	if !filepath.IsAbs(path) {
		if baseDir == "" {
			return nil, "", 0, 0, fmt.Errorf("无基准目录，无法解析相对路径")
		}
		path = filepath.Join(baseDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", 0, 0, err
	}
	return imageInfo(data)
}

func decodeDataURI(source string) ([]byte, string, int, int, error) {
	comma := strings.IndexByte(source, ',')
	if comma < 0 {
		return nil, "", 0, 0, fmt.Errorf("非法的 data URI")
	}
	header := source[:comma]
	if !strings.Contains(strings.ToLower(header), ";base64") {
		return nil, "", 0, 0, fmt.Errorf("仅支持 base64 编码的 data URI")
	}
	data, err := base64.StdEncoding.DecodeString(source[comma+1:])
	if err != nil {
		return nil, "", 0, 0, err
	}
	return imageInfo(data)
}

// imageInfo 校验图片可解码，并归一化为 OFD 支持的 PNG/JPEG。
func imageInfo(data []byte) ([]byte, string, int, int, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, err
	}
	switch strings.ToLower(format) {
	case "png":
		return data, "PNG", config.Width, config.Height, nil
	case "jpeg":
		return data, "JPEG", config.Width, config.Height, nil
	default:
		return nil, "", 0, 0, fmt.Errorf("不支持的图片格式 %q", format)
	}
}
