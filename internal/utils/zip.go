package utils

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

type ZipFileCache struct {
	reader  *zip.Reader
	fileMap map[string]*zip.File
	once    sync.Once
	mu      sync.RWMutex
}

// NewZipFileCache 创建ZIP文件缓存
func NewZipFileCache(reader *zip.Reader) *ZipFileCache {
	return &ZipFileCache{
		reader: reader,
	}
}

// GetOrCreateFileMap 获取或创建文件映射
func (p *ZipFileCache) GetOrCreateFileMap() map[string]*zip.File {
	p.once.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		fileMap := make(map[string]*zip.File, len(p.reader.File))
		for _, file := range p.reader.File {
			fileMap[file.Name] = file
		}
		p.fileMap = fileMap
	})

	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.fileMap
}

// FindFile 查找文件（使用缓存映射）
func (p *ZipFileCache) FindFile(fileName string) (*zip.File, error) {
	fileMap := p.GetOrCreateFileMap()

	if file, ok := fileMap[strings.TrimPrefix(fileName, "/")]; ok {
		return file, nil
	}

	return nil, fmt.Errorf("%w: %s", os.ErrNotExist, fileName)
}

// ParseXMLContent 解析XML文件内容
func (p *ZipFileCache) ParseXMLContent(fileName string, target interface{}) error {
	zf, err := p.FindFile(fileName)
	if err != nil {
		return fmt.Errorf("查找文档失败: %w", err)
	}
	rc, err := zf.Open()
	if err != nil {
		return fmt.Errorf("打开文档失败: %w", err)
	}
	defer rc.Close()

	decoder := xml.NewDecoder(io.LimitReader(rc, int64(zf.UncompressedSize64)+1024))
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("解析XML失败: %w", err)
	}

	return nil
}

func (p *ZipFileCache) ParseImage(fileName string) (image.Image, error) {
	zf, err := p.FindFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("查找图像失败: %w", err)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, fmt.Errorf("打开文档失败: %w", err)
	}
	defer rc.Close()

	var img image.Image
	img, _, err = image.Decode(rc)
	if err != nil {
		return nil, err
	}
	return img, nil
}
func (p *ZipFileCache) ParseContent(fileName string) ([]byte, error) {
	zf, err := p.FindFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("查找文件失败: %w", err)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, fmt.Errorf("打开文档失败: %w", err)
	}
	defer rc.Close()

	return io.ReadAll(rc)
}

func ExtractFirstImage(file string) (image.Image, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		// 简单的扩展名检查
		ext := strings.ToLower(filepath.Ext(f.Name))
		if IsImageExtension(ext) {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			defer rc.Close()

			// 使用通用解码器
			img, format, err := image.Decode(rc)
			if err != nil {
				fmt.Printf("解码失败 %s: %v\n", f.Name, err)
				continue
			}

			fmt.Printf("成功解码: %s (格式: %s)\n", f.Name, format)
			return img, nil
		}
	}

	return nil, fmt.Errorf("未找到图片")
}

// 支持的图片格式
var imageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
	".tiff": true,
	".tif":  true,
}

func IsImageExtension(ext string) bool {
	return imageExtensions[ext]
}
