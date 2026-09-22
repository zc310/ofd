package creator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// DataSource 表示 OFD 包内资源的惰性数据来源，用于在创建超大 OFD 时避免把
// 大资源整体读入内存。带 Source 的资源优先使用 Source，忽略同名的 Data。
//
// 实现必须支持重复 Open，因为创建过程需要多次读取同一来源（摘要、格式嗅探、
// 实际写入）。
type DataSource interface {
	// Open 返回从头读取资源的 ReadCloser，调用方负责关闭。
	Open() (io.ReadCloser, error)
	// Size 返回资源字节数；未知时返回负数。
	Size() int64
}

// FileDataSource 返回从磁盘文件按需读取的 DataSource。
func FileDataSource(path string) DataSource {
	return fileDataSource{path: path}
}

// BytesDataSource 返回从内存字节读取的 DataSource。
func BytesDataSource(data []byte) DataSource {
	return bytesDataSource{data: data}
}

type bytesDataSource struct{ data []byte }

func (s bytesDataSource) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func (s bytesDataSource) Size() int64 {
	return int64(len(s.data))
}

type fileDataSource struct{ path string }

func (s fileDataSource) Open() (io.ReadCloser, error) {
	return os.Open(s.path)
}

func (s fileDataSource) Size() int64 {
	info, err := os.Stat(s.path)
	if err != nil {
		return -1
	}
	return info.Size()
}

// readDataSource 把来源整体读入内存，仅用于需要完整字节的场景。
func readDataSource(source DataSource) ([]byte, error) {
	if source == nil {
		return nil, nil
	}
	reader, err := source.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// prefixDataSource 读取来源开头最多 length 字节，用于格式嗅探。
func prefixDataSource(source DataSource, length int) ([]byte, error) {
	reader, err := source.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	buffer := make([]byte, length)
	read, err := io.ReadFull(reader, buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return buffer[:read], nil
}

// resourceDigest 返回资源的 SHA-256 摘要（十六进制）。提供 Source 时流式计算，
// 不会把整个资源读入内存。
func resourceDigest(data []byte, source DataSource) (string, error) {
	if source == nil {
		digest := sha256.Sum256(data)
		return hex.EncodeToString(digest[:]), nil
	}
	reader, err := source.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// resourceSize 返回资源字节数；来源未知大小时返回 0。
func resourceSize(data []byte, source DataSource) int64 {
	if source == nil {
		return int64(len(data))
	}
	size := source.Size()
	if size < 0 {
		return 0
	}
	return size
}

// hasResource 判断资源是否通过 Data 或 Source 提供了数据。
func hasResource(data []byte, source DataSource) bool {
	return len(data) > 0 || source != nil
}

// hasEmbeddedFonts 判断文档是否声明了需要嵌入的字体数据。
func hasEmbeddedFonts(document Document) bool {
	for _, font := range document.Fonts {
		if hasResource(font.Data, font.Source) {
			return true
		}
	}
	return false
}
