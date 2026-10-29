// Package parser 提供 OFD 文件的打开、解析和文档对象访问能力。
package parser

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

// OFD 表示一个OFD文档解析器
type OFD struct {
	models.OFD

	fileCache *container.Package

	Documents     []*Document
	mainDocuments []*Document
	options       Options
}

const (
	rootDocument = spec.RootDocument
)

const defaultMaxInputBytes = 512 << 20

var (
	errTooManyOptions = errors.New("最多传入一组 Options")
	errNegativeLimits = errors.New("页面缓存和输入大小限制不能为负数")
)

// Options 控制解析器的页面缓存和输入大小限制。
type Options struct {
	// PageCacheCapacity 是页面缓存最多保留的页面数量，0 表示使用默认值。
	PageCacheCapacity int
	// PageCacheBytes 是页面缓存允许使用的估算字节数，0 表示使用默认值。
	PageCacheBytes int64
	// MaxInputBytes 是原始 OFD 输入允许的最大字节数，0 表示使用默认值。
	MaxInputBytes int64
}

// NewOFD 打开 OFD 文档；省略 options 时使用默认配置，最多传入一组 Options。
func NewOFD(input any, options ...Options) (*OFD, error) {
	if len(options) > 1 {
		return nil, errTooManyOptions
	}
	var config Options
	if len(options) == 1 {
		config = options[0]
	}
	if config.PageCacheCapacity < 0 || config.PageCacheBytes < 0 || config.MaxInputBytes < 0 {
		return nil, errNegativeLimits
	}
	if config.MaxInputBytes == 0 {
		config.MaxInputBytes = defaultMaxInputBytes
	}
	var ofd OFD
	ofd.options = config
	return &ofd, ofd.Open(input)
}

// Open 打开OFD文件，支持文件路径、字节数据或通用 io.Reader。
// 对于 io.Reader，Open 会读取全部内容，但不会关闭传入的 reader。
func (p *OFD) Open(input any) error {
	switch v := input.(type) {
	case container.PackageSource:
		if source, ok := input.(interface{ InputSize() int64 }); ok && p.options.MaxInputBytes > 0 && source.InputSize() > p.options.MaxInputBytes {
			return categorized(ErrInputTooLarge, "OFD 输入视图的原始 ZIP 超过大小限制")
		}
		pkg, err := v.OFDPackage()
		if err != nil {
			return err
		}
		return p.openPackage(pkg)
	case string:
		return p.openFromFile(v)
	case []byte:
		return p.openFromBytes(v)
	case io.Reader:
		var source io.Reader = v
		// 最大 int64 不能再加 1；此时 Reader 的可表示长度本身已受 int64 限制。
		if p.options.MaxInputBytes > 0 && p.options.MaxInputBytes < 1<<63-1 {
			source = io.LimitReader(v, p.options.MaxInputBytes+1)
		}
		data, err := io.ReadAll(source)
		if err != nil {
			return fmt.Errorf("读取OFD内容失败: %w", err)
		}
		if p.options.MaxInputBytes > 0 && int64(len(data)) > p.options.MaxInputBytes {
			return categorized(ErrInputTooLarge, fmt.Sprintf("OFD 数据超过大小限制 %d MB", p.options.MaxInputBytes>>20))
		}
		fileCache, err := container.OpenBytes(data)
		if err != nil {
			return fmt.Errorf("读取OFD内容失败: %w", err)
		}
		return p.openPackage(fileCache)
	default:
		return fmt.Errorf("不支持的类型: %T, 请提供文件路径(string)、文件数据([]byte)或 io.Reader", input)
	}
}

// openFromFile 从文件路径打开OFD文件
func (p *OFD) openFromFile(filePath string) error {
	cleanPath := filepath.Clean(filePath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("文件路径验证失败: %w", err)
	}
	if p.options.MaxInputBytes > 0 && info.Size() > p.options.MaxInputBytes {
		return categorized(ErrInputTooLarge, fmt.Sprintf("OFD 文件超过大小限制 %d MB", p.options.MaxInputBytes>>20))
	}

	fileCache, err := container.OpenFile(cleanPath)
	if err != nil {
		return fmt.Errorf("打开OFD文件失败: %w", err)
	}

	return p.openPackage(fileCache)
}

// openFromBytes 从字节数据打开OFD文件
func (p *OFD) openFromBytes(data []byte) error {
	if p.options.MaxInputBytes > 0 && int64(len(data)) > p.options.MaxInputBytes {
		return categorized(ErrInputTooLarge, fmt.Sprintf("OFD 数据超过大小限制 %d MB", p.options.MaxInputBytes>>20))
	}
	fileCache, err := container.OpenBytes(data)
	if err != nil {
		return fmt.Errorf("从字节数据创建 ZIP 包失败: %w", err)
	}

	return p.openPackage(fileCache)
}

// openPackage 解析候选 ZIP，并在成功后一次性替换当前文档状态。
// 这样重复调用 Open 时，解析失败不会破坏当前已打开的文档。
func (p *OFD) openPackage(fileCache *container.Package) (err error) {
	if fileCache == nil {
		return fmt.Errorf("打开OFD文件失败: 包访问对象为空")
	}
	root, documents, err := parsePackage(fileCache, p.options)
	if err != nil {
		_ = fileCache.Close()
		return err
	}
	if err = p.Close(); err != nil {
		_ = fileCache.Close()
		return err
	}

	p.OFD = root
	p.Documents = documents
	p.mainDocuments = append([]*Document(nil), documents...)
	p.fileCache = fileCache
	return nil
}

// Close 关闭OFD解析器并释放资源
func (p *OFD) Close() error {
	var err error
	for _, document := range p.Documents {
		if document != nil {
			document.clearCaches()
		}
	}
	for scope, document := range p.mainDocuments {
		if document != nil && (scope >= len(p.Documents) || document != p.Documents[scope]) {
			document.clearCaches()
		}
	}
	if p.fileCache != nil {
		err = p.fileCache.Close()
		p.fileCache = nil
	}
	p.Documents = nil
	p.mainDocuments = nil
	p.OFD = models.OFD{}
	if err != nil {
		return fmt.Errorf("关闭OFD文件失败: %w", err)
	}
	return nil
}

func parsePackage(fileCache *container.Package, options Options) (models.OFD, []*Document, error) {
	if err := checkEncryption(fileCache); err != nil {
		return models.OFD{}, nil, err
	}
	var root models.OFD
	if err := fileCache.ReadXML(rootDocument, &root); err != nil {
		return models.OFD{}, nil, err
	}

	documents := make([]*Document, 0, len(root.DocBodies))
	for _, body := range root.DocBodies {
		document := &Document{
			pageCacheSize:  options.PageCacheCapacity,
			pageCacheBytes: options.PageCacheBytes,
		}
		document.Init(fileCache, body.DocRoot)
		if err := document.parse(body); err != nil {
			return models.OFD{}, nil, err
		}
		if err := document.ParseSigns(body.Signatures); err != nil {
			return models.OFD{}, nil, err
		}
		documents = append(documents, document)
	}
	return root, documents, nil
}
