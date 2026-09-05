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
		if p.reader == nil {
			p.fileMap = map[string]*zip.File{}
			return
		}
		fileMap := make(map[string]*zip.File, len(p.reader.File))
		for _, file := range p.reader.File {
			fileMap[strings.TrimLeft(file.Name, "/")] = file
		}
		p.fileMap = fileMap
	})
	return p.fileMap
}

// FindFile 查找文件（使用缓存映射）
func (p *ZipFileCache) FindFile(fileName string) (*zip.File, error) {
	fileMap := p.GetOrCreateFileMap()
	name := strings.TrimLeft(fileName, "/")
	if file, ok := fileMap[name]; ok {
		return file, nil
	}

	return nil, fmt.Errorf("%w: %s", os.ErrNotExist, name)
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

	decoder := xml.NewDecoder(io.LimitReader(rc, xmlReadLimit(zf.UncompressedSize64)))
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

func xmlReadLimit(size uint64) int64 {
	const extra = uint64(1024)
	const maxInt64 = uint64(1<<63 - 1)
	if size > maxInt64-extra {
		return int64(maxInt64)
	}
	return int64(size + extra)
}

func ExtractFirstImage(file string) (image.Image, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if IsImageExtension(ext) {
			if img, ok := decodeZipImage(f); ok {
				return img, nil
			}
		}
	}

	return nil, fmt.Errorf("未找到图片")
}

func decodeZipImage(file *zip.File) (img image.Image, ok bool) {
	rc, err := file.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()
	img, _, err = image.Decode(rc)
	return img, err == nil
}

// 支持的图片格式
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

func IsImageExtension(ext string) bool {
	_, ok := imageExtensions[strings.ToLower(ext)]
	return ok
}
