package creator

import (
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zip"
)

const (
	// DefaultMaxEntries 是默认的最大条目数。
	DefaultMaxEntries = 10000
	// DefaultMaxEntryBytes 是默认的单条解压上限（64MB）。
	DefaultMaxEntryBytes = int64(64 << 20)
	// DefaultMaxTotalBytes 是默认的解压总字节上限（512MB）。
	DefaultMaxTotalBytes = int64(512 << 20)
)

// Limits 限制 OFD 包读写的规模，避免恶意或异常文档造成的解压放大。
type Limits struct {
	// MaxEntries 是允许搬运的最大条目数，0 表示默认 10000。
	MaxEntries int
	// MaxEntryBytes 是单个条目解压后的最大字节数，0 表示默认 64MB。
	MaxEntryBytes int64
	// MaxTotalBytes 是所有条目解压后的总字节上限，0 表示默认 512MB。
	MaxTotalBytes int64
}

// SignatureMode 控制处理 OFD 包时签名文件的去向。
type SignatureMode string

const (
	// SignatureDrop 删除签名目录，产出无签名文档。
	SignatureDrop SignatureMode = "drop"
	// SignaturePreserve 原样保留签名文件。
	SignaturePreserve SignatureMode = "preserve"
	// SignatureRewrite 保留签名文件但重写其中的包内绝对路径，签名值通常失效。
	SignatureRewrite SignatureMode = "rewrite"
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

// EntryMethod 返回 ZIP 条目的压缩方式。mode 为 "store" 或 "deflate" 时
// 强制指定；其他值按扩展名自动选择：本身已压缩的格式使用 Store，其余使用
// Deflate。
func EntryMethod(name string, mode CompressionMode) uint16 {
	switch mode {
	case CompressionStore:
		return zip.Store
	case CompressionDeflate:
		return zip.Deflate
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".jxl",
		".mp3", ".mp4", ".m4a", ".aac", ".ogg", ".oga", ".opus", ".wav",
		".flac", ".mpg", ".mpeg", ".avi", ".mkv", ".mov", ".webm",
		".m4v", ".wma", ".pdf", ".zip", ".gz", ".bz2", ".xz", ".7z", ".rar",
		".docx", ".xlsx", ".pptx", ".ofd":
		return zip.Store
	default:
		return zip.Deflate
	}
}

// NormalizeCompression 返回合法化的压缩策略：空值使用 CompressionAuto，
// 不支持的值返回错误。
func NormalizeCompression(mode CompressionMode) (CompressionMode, error) {
	if mode == "" {
		return CompressionAuto, nil
	}
	switch mode {
	case CompressionAuto, CompressionDeflate, CompressionStore:
		return mode, nil
	default:
		return "", fmt.Errorf("不支持的 ZIP 压缩策略: %q", mode)
	}
}

// ValidateLimits 校验解压规模限制不能为负数。
func ValidateLimits(limits Limits) error {
	if limits.MaxEntries < 0 || limits.MaxEntryBytes < 0 || limits.MaxTotalBytes < 0 {
		return errors.New("规模限制不能为负数")
	}
	return nil
}

// LimitWriter 在复制过程中同时限制单条与总解压字节数，防止声明的解压
// 大小与实际不符（如 zip bomb）。
type LimitWriter struct {
	writer         io.Writer
	entryRemaining int64
	totalRemaining *int64
	name           string
	maxTotal       int64
}

// NewLimitWriter 返回带限制的 writer；totalRemaining 指向调用方的总预算，
// 每次写入后同步扣减。
func NewLimitWriter(writer io.Writer, name string, entryRemaining, maxTotal int64, totalRemaining *int64) *LimitWriter {
	return &LimitWriter{
		writer:         writer,
		entryRemaining: entryRemaining,
		totalRemaining: totalRemaining,
		name:           name,
		maxTotal:       maxTotal,
	}
}

func (l *LimitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.entryRemaining {
		return 0, fmt.Errorf("条目 %s 解压后超过单条大小上限", l.name)
	}
	if int64(len(p)) > *l.totalRemaining {
		return 0, fmt.Errorf("解压总大小超过上限 %d 字节", l.maxTotal)
	}
	n, err := l.writer.Write(p)
	l.entryRemaining -= int64(n)
	*l.totalRemaining -= int64(n)
	return n, err
}

// CheckSize 校验内存条目 size 是否在单条与总预算内，并在通过后扣减总预算。
func CheckSize(name string, size, maxEntryBytes, maxTotalBytes int64, remaining *int64) error {
	if size > maxEntryBytes {
		return fmt.Errorf("条目 %s 解压后 %d 字节，超过单条上限 %d 字节", name, size, maxEntryBytes)
	}
	if size > *remaining {
		return fmt.Errorf("解压总大小超过上限 %d 字节", maxTotalBytes)
	}
	*remaining -= size
	return nil
}

// IsSignatureEntry 判断条目是否为签名清单、签名文件或签名目录内的条目，
// 覆盖标准 Signatures/ 布局与历史 Signs/ 布局。
func IsSignatureEntry(name string) bool {
	switch path.Base(name) {
	case "Signatures.xml", "Signs.xml", "Signature.xml":
		return true
	}
	return strings.Contains(name, "/Signatures/") || strings.Contains(name, "/Signs/")
}

// InSignatureDir 判断条目是否位于给出的任一签名目录内；空目录忽略。
func InSignatureDir(name string, dirs ...string) bool {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if name == dir || strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	return false
}
