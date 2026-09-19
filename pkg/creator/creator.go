// Package creator 根据高层文档模型创建 OFD 文件包。
package creator

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zip"
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
	Compression           CompressionMode
	Deterministic         bool
	PreserveEmbeddedFonts bool
	// CompleteTextCodeDeltas 按字体度量自动补全缺失的 DeltaX 和 DeltaY。
	CompleteTextCodeDeltas bool
}

// Create 将完整的 OFD ZIP 文件包写入 w。
func Create(document Document, w io.Writer) error {
	return CreateWithOptions(document, w, CreateOptions{Compression: CompressionAuto})
}

// CreateWithOptions 按指定选项将完整的 OFD ZIP 文件包写入 w。
func CreateWithOptions(document Document, w io.Writer, options CreateOptions) error {
	return createWithPages(document, slicePages{pages: document.Pages}, w, options)
}

// CreateWithPages 使用 PageProvider 按需提供的页面创建 OFD，用于页数或页模型
// 超过内存的场景。meta 提供文档元数据与资源，meta.Pages 必须为空。
//
// 因为字体子集化需要改写页内字形编号，流式页面必须设置
// CreateOptions.PreserveEmbeddedFonts 为 true，或预先自行完成字体子集化。
func CreateWithPages(meta Document, pages PageProvider, w io.Writer) error {
	return CreateWithPagesOptions(meta, pages, w, CreateOptions{Compression: CompressionAuto})
}

// CreateWithPagesOptions 按指定选项使用 PageProvider 创建 OFD。
func CreateWithPagesOptions(meta Document, pages PageProvider, w io.Writer, options CreateOptions) error {
	return createWithPages(meta, pages, w, options)
}

func createWithPages(document Document, pages PageProvider, w io.Writer, options CreateOptions) error {
	if w == nil {
		return errors.New("OFD 输出写入器为空")
	}
	if pages == nil {
		return errors.New("OFD 页面提供者为空")
	}
	if options.Compression == "" {
		options.Compression = CompressionAuto
	}
	if options.Compression != CompressionAuto && options.Compression != CompressionDeflate && options.Compression != CompressionStore {
		return fmt.Errorf("不支持的 ZIP 压缩策略: %q", options.Compression)
	}
	if _, ok := pages.(slicePages); !ok && !options.PreserveEmbeddedFonts && hasEmbeddedFonts(document) {
		return errors.New("流式页面创建需要设置 CreateOptions.PreserveEmbeddedFonts，或预先完成字体子集化")
	}
	state, err := prepare(document, pages, options)
	if err != nil {
		return err
	}
	defer state.clearPatternCaches()

	archive := zip.NewWriter(w)
	sink := newZipSink(archive, options, signatureReferenceTargets(state))
	if err := generatePackage(state, sink); err != nil {
		_ = archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("关闭 OFD ZIP 包失败: %w", err)
	}
	return nil
}

// CreateFileWithPages 使用 PageProvider 在 filename 指定的位置创建 OFD 文件包。
func CreateFileWithPages(meta Document, pages PageProvider, filename string, options CreateOptions) (err error) {
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
	return CreateWithPagesOptions(meta, pages, file, options)
}

// MarshalWithPages 按指定选项返回使用 PageProvider 创建的完整 OFD 字节数据。
func MarshalWithPages(meta Document, pages PageProvider, options CreateOptions) ([]byte, error) {
	var buffer bytes.Buffer
	if err := CreateWithPagesOptions(meta, pages, &buffer, options); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
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
