package creator

import (
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
)

const (
	// DefaultMaxEntries 是默认的最大条目数。
	DefaultMaxEntries = container.DefaultMaxEntries
	// DefaultMaxEntryBytes 是默认的单条解压上限（64MB）。
	DefaultMaxEntryBytes = container.DefaultMaxEntryBytes
	// DefaultMaxTotalBytes 是默认的解压总字节上限（512MB）。
	DefaultMaxTotalBytes = container.DefaultMaxTotalBytes
)

// Limits 限制 OFD 包读写的规模，避免恶意或异常文档造成的解压放大。
type Limits = container.Limits

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
type CompressionMode = container.CompressionMode

const (
	// CompressionAuto 对已压缩格式使用 Store，其他文件使用 Deflate。
	CompressionAuto = container.CompressionAuto
	// CompressionDeflate 对所有条目使用 Deflate。
	CompressionDeflate = container.CompressionDeflate
	// CompressionStore 对所有条目使用 Store。
	CompressionStore = container.CompressionStore
)

// EntryMethod 返回 ZIP 条目的压缩方式。mode 为 "store" 或 "deflate" 时
// 强制指定；其他值按扩展名自动选择：本身已压缩的格式使用 Store，其余使用 Deflate。
func EntryMethod(name string, mode CompressionMode) uint16 {
	return container.EntryMethod(name, mode)
}

// NormalizeCompression 返回合法化的压缩策略：空值使用 CompressionAuto，
// 不支持的值返回错误。
func NormalizeCompression(mode CompressionMode) (CompressionMode, error) {
	return container.NormalizeCompression(mode)
}

// DefaultCompressionLevel 是 DEFLATE 的默认压缩级别，与 klauspost zip 的默认一致。
const DefaultCompressionLevel = container.DefaultCompressionLevel

// NormalizeCompressionLevel 校验 DEFLATE 压缩级别：0 使用默认级别，显式范围为 1–9。
func NormalizeCompressionLevel(level int) (int, error) {
	return container.NormalizeCompressionLevel(level)
}

// ApplyCompressionLevel 在首次创建 Deflate 条目前注册压缩器。
// level 为 0 或默认级别时使用 ZIP 库的默认实现；调用方应先校验级别。
func ApplyCompressionLevel(w *zip.Writer, level int) {
	container.ApplyCompressionLevel(w, level)
}

// ValidateLimits 校验解压规模限制不能为负数。
func ValidateLimits(limits Limits) error {
	return container.ValidateLimits(limits)
}

// LimitWriter 在复制过程中同时限制单条与总解压字节数。
type LimitWriter = container.LimitWriter

// NewLimitWriter 返回带限制的写入器；每次写入后同步扣减 totalRemaining 指向的总预算。
func NewLimitWriter(writer io.Writer, name string, entryRemaining, maxTotal int64, totalRemaining *int64) *LimitWriter {
	return container.NewLimitWriter(writer, name, entryRemaining, maxTotal, totalRemaining)
}

// CheckSize 校验内存条目大小，并在通过后扣减总预算。
func CheckSize(name string, size, maxEntryBytes, maxTotalBytes int64, remaining *int64) error {
	return container.CheckSize(name, size, maxEntryBytes, maxTotalBytes, remaining)
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
