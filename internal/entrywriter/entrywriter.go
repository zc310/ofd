// Package entrywriter 提供带规模限制与确定性选项的 OFD ZIP 条目写入器，
// 供 ZIP 级处理工具（如 pkg/merge、pkg/replace）复用。
package entrywriter

import (
	"fmt"
	"io"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/creator"
)

// Config 控制条目写入的压缩策略与规模限制。
type Config struct {
	// Compression 是输出 ZIP 的压缩策略，空值使用 creator.CompressionAuto。
	Compression creator.CompressionMode
	// CompressionLevel 是 DEFLATE 压缩级别，0 使用默认级别 5，显式范围
	// 1（最快）到 9（最紧凑）。仅对实际使用 Deflate 的条目生效。
	CompressionLevel int
	// Deterministic 使用固定 ZIP 时间，生成可复现的结果。
	Deterministic bool
	// Limits 限制输出规模，零值使用 creator 的默认限制。
	Limits creator.Limits
	// OnWarning 可选，接收非致命提示。
	OnWarning func(string)
}

// Writer 向归档写入受限的 ZIP 条目，拒绝非法或重复路径，并遵守压缩与
// 确定性设置。
type Writer struct {
	archive    *zip.Writer
	config     Config
	seen       map[string]bool
	entryCount int
	remaining  int64
}

// New 创建写入器并套用默认规模限制。调用方负责在写完后关闭 archive。
// 若 CompressionLevel 无效，回退默认级别并通过 OnWarning 上报。
func New(archive *zip.Writer, config Config) *Writer {
	if config.Compression == "" {
		config.Compression = creator.CompressionAuto
	}
	if config.Limits.MaxEntries == 0 {
		config.Limits.MaxEntries = creator.DefaultMaxEntries
	}
	if config.Limits.MaxEntryBytes == 0 {
		config.Limits.MaxEntryBytes = creator.DefaultMaxEntryBytes
	}
	if config.Limits.MaxTotalBytes == 0 {
		config.Limits.MaxTotalBytes = creator.DefaultMaxTotalBytes
	}
	var levelErr error
	config.CompressionLevel, levelErr = creator.NormalizeCompressionLevel(config.CompressionLevel)
	if levelErr != nil {
		config.CompressionLevel = 0
	}
	creator.ApplyCompressionLevel(archive, config.CompressionLevel)
	w := &Writer{
		archive:   archive,
		config:    config,
		seen:      make(map[string]bool),
		remaining: config.Limits.MaxTotalBytes,
	}
	if levelErr != nil {
		w.Warn(fmt.Sprintf("无效的 DEFLATE 压缩级别，使用默认: %v", levelErr))
	}
	return w
}

// MaxEntryBytes 返回当前单条解压字节上限。
func (w *Writer) MaxEntryBytes() int64 {
	return w.config.Limits.MaxEntryBytes
}

// Warn 通过 Config.OnWarning 上报非致命提示。
func (w *Writer) Warn(message string) {
	if w.config.OnWarning != nil {
		w.config.OnWarning(message)
	}
}

// Write 按压缩策略与确定性选项写入一个内存条目。
func (w *Writer) Write(name string, data []byte) error {
	if err := w.reserve(name); err != nil {
		return err
	}
	if err := w.checkSize(name, int64(len(data))); err != nil {
		return err
	}
	file, err := w.archive.CreateHeader(w.header(name))
	if err != nil {
		return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", name, err)
	}
	return nil
}

// WriteSource 流式写入输入包中的条目，避免资源整体驻留内存。
func (w *Writer) WriteSource(pkg *core.Package, entry core.Entry, name string) error {
	if err := w.reserve(name); err != nil {
		return err
	}
	if entry.UncompressedSize > uint64(w.config.Limits.MaxEntryBytes) {
		return fmt.Errorf("条目 %s 声明解压后 %d 字节，超过单条上限 %d 字节", name, entry.UncompressedSize, w.config.Limits.MaxEntryBytes)
	}
	if int64(entry.UncompressedSize) > w.remaining {
		return fmt.Errorf("解压总大小超过上限 %d 字节", w.config.Limits.MaxTotalBytes)
	}
	reader, err := pkg.OpenEntry(entry)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	file, err := w.archive.CreateHeader(w.header(name))
	if err != nil {
		return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
	}
	counter := creator.NewLimitWriter(file, name, w.config.Limits.MaxEntryBytes, w.config.Limits.MaxTotalBytes, &w.remaining)
	if _, err := io.Copy(counter, reader); err != nil {
		return err
	}
	return nil
}

func (w *Writer) header(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: creator.EntryMethod(name, w.config.Compression)}
	if w.config.Deterministic {
		header.Modified = time.Unix(0, 0).UTC()
	}
	return header
}

func (w *Writer) reserve(name string) error {
	if err := core.ValidateEntryName(name); err != nil {
		return err
	}
	if w.seen[name] {
		return fmt.Errorf("OFD 包条目路径重复: %s", name)
	}
	w.seen[name] = true
	if w.entryCount >= w.config.Limits.MaxEntries {
		return fmt.Errorf("条目数超过上限 %d", w.config.Limits.MaxEntries)
	}
	w.entryCount++
	return nil
}

func (w *Writer) checkSize(name string, size int64) error {
	return creator.CheckSize(name, size, w.config.Limits.MaxEntryBytes, w.config.Limits.MaxTotalBytes, &w.remaining)
}
