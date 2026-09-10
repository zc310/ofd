// Package creator 根据高层文档模型创建 OFD 文件包。
package creator

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ofNamespace = "http://www.ofdspec.org/2016"
	docDir      = "Doc_0"
	resDir      = docDir + "/Res"
	maxOFDID    = uint64(^uint32(0))
)

// CompressionMode 表示 OFD ZIP 条目的压缩策略。
type CompressionMode string

const (
	// CompressionAuto 对已压缩格式使用 Store，其他文件使用 Deflate。
	CompressionAuto CompressionMode = "auto"
	// CompressionDeflate 对所有条目使用 Deflate。
	CompressionDeflate CompressionMode = "deflate"
	// CompressionStore 对所有条目使用 Store。
	CompressionStore CompressionMode = "store"
)

// CreateOptions 控制 OFD ZIP 包的生成方式。
type CreateOptions struct {
	Compression   CompressionMode
	Deterministic bool
	// CompleteTextCodeDeltas 按字体度量自动补全缺失的 DeltaX 和 DeltaY。
	CompleteTextCodeDeltas bool
}

// Create 将完整的 OFD ZIP 文件包写入 w。
func Create(document Document, w io.Writer) error {
	return CreateWithOptions(document, w, CreateOptions{Compression: CompressionAuto})
}

// CreateWithOptions 按指定选项将完整的 OFD ZIP 文件包写入 w。
func CreateWithOptions(document Document, w io.Writer, options CreateOptions) error {
	if w == nil {
		return errors.New("OFD 输出写入器为空")
	}
	if options.Compression == "" {
		options.Compression = CompressionAuto
	}
	if options.Compression != CompressionAuto && options.Compression != CompressionDeflate && options.Compression != CompressionStore {
		return fmt.Errorf("不支持的 ZIP 压缩策略: %q", options.Compression)
	}
	state, err := buildWithOptions(document, options)
	if err != nil {
		return err
	}

	archive := zip.NewWriter(w)
	for _, entry := range state.entries {
		header := &zip.FileHeader{
			Name:   entry.name,
			Method: zipEntryMethod(entry.name, options.Compression),
		}
		if options.Deterministic {
			header.Modified = time.Unix(0, 0).UTC()
		}
		file, createErr := archive.CreateHeader(header)
		if createErr != nil {
			_ = archive.Close()
			return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", entry.name, createErr)
		}
		if _, writeErr := file.Write(entry.data); writeErr != nil {
			_ = archive.Close()
			return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", entry.name, writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("关闭 OFD ZIP 包失败: %w", err)
	}
	return nil
}

func zipEntryMethod(name string, mode CompressionMode) uint16 {
	if mode == CompressionStore {
		return zip.Store
	}
	if mode == CompressionDeflate {
		return zip.Deflate
	}
	extension := strings.ToLower(filepath.Ext(name))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".jxl",
		".mp3", ".mp4", ".m4a", ".aac", ".ogg", ".oga", ".opus", ".wav",
		".flac", ".mpg", ".mpeg", ".avi", ".mkv", ".mov", ".webm",
		".m4v", ".wma", ".pdf", ".zip", ".gz", ".bz2", ".xz", ".7z", ".rar":
		return zip.Store
	default:
		return zip.Deflate
	}
}

// CreateFile 在 filename 指定的位置创建 OFD 文件包。
func CreateFile(document Document, filename string) (err error) {
	if strings.TrimSpace(filename) == "" {
		return errors.New("OFD 输出文件名为空")
	}
	file, err := os.Create(filepath.Clean(filename))
	if err != nil {
		return fmt.Errorf("创建 OFD 文件失败: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("关闭 OFD 文件失败: %w", closeErr)
		}
	}()
	return Create(document, file)
}

// Marshal 返回完整 OFD 文件包的字节数据。
func Marshal(document Document) ([]byte, error) {
	return MarshalWithOptions(document, CreateOptions{Compression: CompressionAuto})
}

// MarshalWithOptions 按指定选项返回完整 OFD 文件包的字节数据。
func MarshalWithOptions(document Document, options CreateOptions) ([]byte, error) {
	var buffer bytes.Buffer
	if err := CreateWithOptions(document, &buffer, options); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
