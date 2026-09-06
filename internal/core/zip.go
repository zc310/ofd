package core

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	// ErrReadLimitExceeded 表示读取的解压后内容超过了大小限制。
	ErrReadLimitExceeded = errors.New("读取内容超过大小限制")
	// ErrPackageClosed 表示 ZIP 包已经关闭。
	ErrPackageClosed = errors.New("ZIP 包已经关闭")
)

const maxInt64 = int64(1<<63 - 1)

// Entry 描述 OFD ZIP 包中的一个条目。
type Entry struct {
	// Name 是 ZIP 条目的原始名称。
	Name string
	// Path 是去除开头斜杠后的包内查找路径。
	Path string
	// CompressedSize 是条目的压缩后大小。
	CompressedSize uint64
	// UncompressedSize 是条目的解压后大小。
	UncompressedSize uint64
	// Method 是 ZIP 压缩方法编号。
	Method uint16
	// CRC32 是条目的 CRC-32 校验值。
	CRC32 uint32
	// Modified 是条目的修改时间。
	Modified time.Time
	// Mode 是条目的文件模式。
	Mode fs.FileMode
	// IsDir 表示条目是否为目录。
	IsDir bool

	token *packageToken
	index int
}

type packageToken struct {
	_ byte
}

// Package 提供 OFD ZIP 包的条目索引和内容读取能力。
type Package struct {
	reader *zip.Reader
	closer io.Closer
	token  *packageToken

	fileMap map[string]int
	entries []Entry
	once    sync.Once

	mu       sync.RWMutex
	closed   bool
	closeErr error
}

func newPackage(reader *zip.Reader, closer io.Closer) *Package {
	return &Package{reader: reader, closer: closer, token: &packageToken{}}
}

// OpenFile 从文件路径打开 OFD ZIP 包。
// 返回的 Package 使用完毕后应调用 Close。
func OpenFile(filename string) (*Package, error) {
	reader, err := zip.OpenReader(filename)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, fmt.Errorf("打开 ZIP 包失败: %w", err)
	}
	if reader == nil {
		return nil, errors.New("打开 ZIP 包失败: ZIP reader 为空")
	}
	// archive/zip 在路径不安全时仍会返回可用 reader；路径策略由上层校验器处理。
	return newPackage(&reader.Reader, reader), nil
}

// OpenBytes 从字节数据打开 OFD ZIP 包。
func OpenBytes(data []byte) (*Package, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("从字节数据创建 ZIP 包失败: %w", err)
	}
	if reader == nil {
		return nil, errors.New("从字节数据创建 ZIP 包失败: ZIP reader 为空")
	}
	return newPackage(reader, nil), nil
}

// OpenReader 从读取器中读取全部数据并打开 OFD ZIP 包。
// 调用方仍负责关闭传入的 reader。
func OpenReader(reader io.Reader) (*Package, error) {
	if reader == nil {
		return nil, errors.New("输入读取器为空")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取 ZIP 包失败: %w", err)
	}
	return OpenBytes(data)
}

// Close 关闭由 OpenFile 创建的 ZIP 包，并释放其文件句柄。
// 对 OpenBytes 和 OpenReader 创建的对象，Close 没有额外操作。
func (p *Package) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if p.closer != nil {
		p.closeErr = p.closer.Close()
	}
	return p.closeErr
}

func (p *Package) ensureIndex() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.reader == nil {
			p.fileMap = map[string]int{}
			p.entries = []Entry{}
			return
		}
		fileMap := make(map[string]int, len(p.reader.File))
		entries := make([]Entry, 0, len(p.reader.File))
		for index, file := range p.reader.File {
			fileMap[lookupName(file.Name)] = index
			entries = append(entries, entryFromZipFile(p.token, index, file))
		}
		p.fileMap = fileMap
		p.entries = entries
	})
}

// Entries 返回 ZIP 包条目的元数据快照，顺序与 ZIP 中的条目顺序一致。
func (p *Package) Entries() []Entry {
	if p == nil {
		return nil
	}
	p.ensureIndex()
	entries := make([]Entry, len(p.entries))
	copy(entries, p.entries)
	return entries
}

// Lookup 查找指定名称的 ZIP 条目元数据。
// 如果多个条目规范化后路径相同，返回 ZIP 中最后出现的条目；全部条目可通过 Entries 获取。
func (p *Package) Lookup(fileName string) (Entry, bool) {
	index, ok := p.lookupIndex(fileName)
	if !ok {
		return Entry{}, false
	}
	return p.entries[index], true
}

// Has 判断 ZIP 包中是否存在指定名称的条目。
func (p *Package) Has(fileName string) bool {
	_, ok := p.lookupIndex(fileName)
	return ok
}

// Open 打开 ZIP 包内的条目，返回解压后的读取器。
func (p *Package) Open(fileName string) (io.ReadCloser, error) {
	if p == nil {
		return nil, fmt.Errorf("打开文件失败: %w", os.ErrNotExist)
	}
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return nil, ErrPackageClosed
	}
	p.ensureIndex()
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, ErrPackageClosed
	}
	index, ok := p.fileMap[lookupName(fileName)]
	if !ok {
		return nil, fmt.Errorf("打开文件失败: %w: %s", os.ErrNotExist, lookupName(fileName))
	}
	file := p.reader.File[index]
	if file.FileInfo().IsDir() {
		return nil, fmt.Errorf("打开文件失败: %s 是目录", lookupName(fileName))
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	return reader, nil
}

// OpenEntry 打开 Entries 返回的指定 ZIP 条目。
// 与 Open 不同，OpenEntry 不会因条目名称重复而切换到其他条目。
func (p *Package) OpenEntry(entry Entry) (io.ReadCloser, error) {
	if p == nil {
		return nil, fmt.Errorf("打开文件失败: %w", os.ErrNotExist)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, ErrPackageClosed
	}
	if entry.token != p.token || p.reader == nil || entry.index < 0 || entry.index >= len(p.reader.File) {
		return nil, fmt.Errorf("打开文件失败: %w: %s", os.ErrNotExist, entry.Path)
	}
	file := p.reader.File[entry.index]
	if file.FileInfo().IsDir() {
		return nil, fmt.Errorf("打开文件失败: %s 是目录", entry.Path)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	return reader, nil
}

// Read 读取 ZIP 包内条目的全部解压后内容。
func (p *Package) Read(fileName string) ([]byte, error) {
	return p.ReadLimit(fileName, 0)
}

// ReadLimit 读取 ZIP 包内条目的解压后内容，并限制最大字节数。
// limit 为 0 时表示不限制。
func (p *Package) ReadLimit(fileName string, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("读取大小限制不能为负数: %d", limit)
	}
	if limit > 0 {
		if entry, ok := p.Lookup(fileName); ok && entry.UncompressedSize > uint64(limit) {
			return nil, ErrReadLimitExceeded
		}
	}

	reader, err := p.Open(fileName)
	if err != nil {
		return nil, err
	}
	data, readErr := readLimit(reader, limit)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("读取文件失败: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("关闭文件失败: %w", closeErr)
	}
	return data, nil
}

// ReadXML 解析 ZIP 包内的 XML 文件。
func (p *Package) ReadXML(fileName string, target any) error {
	entry, ok := p.Lookup(fileName)
	if !ok {
		return fmt.Errorf("读取 XML 文件失败: %w: %s", os.ErrNotExist, lookupName(fileName))
	}
	return p.ReadXMLLimit(fileName, target, xmlReadLimit(entry.UncompressedSize))
}

// ReadXMLLimit 解析 ZIP 包内的 XML 文件，并限制最大字节数。
// limit 为 0 时表示不限制。
func (p *Package) ReadXMLLimit(fileName string, target any, limit int64) error {
	if limit < 0 {
		return fmt.Errorf("XML 大小限制不能为负数: %d", limit)
	}
	if limit > 0 {
		if entry, ok := p.Lookup(fileName); ok && entry.UncompressedSize > uint64(limit) {
			return fmt.Errorf("读取 XML 文件失败: %w", ErrReadLimitExceeded)
		}
	}

	reader, err := p.Open(fileName)
	if err != nil {
		return fmt.Errorf("读取 XML 文件失败: %w", err)
	}

	var source io.Reader = reader
	var limited *io.LimitedReader
	if limit > 0 && limit < maxInt64 {
		limited = &io.LimitedReader{R: reader, N: limit + 1}
		source = limited
	}

	decoder := xml.NewDecoder(source)
	decodeErr := decoder.Decode(target)
	var readErr error
	if limited != nil {
		_, readErr = io.Copy(io.Discard, source)
	}
	closeErr := reader.Close()
	if limited != nil && limited.N == 0 {
		return fmt.Errorf("读取 XML 文件失败: %w", ErrReadLimitExceeded)
	}
	if readErr != nil {
		return fmt.Errorf("读取 XML 文件失败: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭 XML 文件失败: %w", closeErr)
	}
	if decodeErr != nil {
		return fmt.Errorf("解析 XML 失败: %w", decodeErr)
	}
	return nil
}

func (p *Package) lookupIndex(fileName string) (int, bool) {
	if p == nil {
		return 0, false
	}
	p.ensureIndex()
	index, ok := p.fileMap[lookupName(fileName)]
	return index, ok
}

func lookupName(fileName string) string {
	return strings.TrimLeft(fileName, "/")
}

func entryFromZipFile(token *packageToken, index int, file *zip.File) Entry {
	if file == nil {
		return Entry{}
	}
	info := file.FileInfo()
	return Entry{
		Name:             file.Name,
		Path:             lookupName(file.Name),
		CompressedSize:   file.CompressedSize64,
		UncompressedSize: file.UncompressedSize64,
		Method:           file.Method,
		CRC32:            file.CRC32,
		Modified:         file.Modified,
		Mode:             info.Mode(),
		IsDir:            info.IsDir(),
		token:            token,
		index:            index,
	}
}

func readLimit(reader io.Reader, limit int64) ([]byte, error) {
	if limit == 0 {
		return io.ReadAll(reader)
	}
	readSize := limit
	if limit < maxInt64 {
		readSize++
	}
	data, err := io.ReadAll(io.LimitReader(reader, readSize))
	if err != nil {
		return nil, err
	}
	if limit < maxInt64 && int64(len(data)) > limit {
		return nil, ErrReadLimitExceeded
	}
	return data, nil
}

func xmlReadLimit(size uint64) int64 {
	const extra = uint64(1024)
	const maxInt64 = uint64(1<<63 - 1)
	if size > maxInt64-extra {
		return int64(maxInt64)
	}
	return int64(size + extra)
}
