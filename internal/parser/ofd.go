package parser

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

// OFD 表示一个OFD文档解析器
type OFD struct {
	models.OFD
	reader    *zip.ReadCloser
	fileCache *core.ZipFileCache

	Documents []*Document
}

const (
	rootDocument = "OFD.xml"
)

func NewOFD(input any) (*OFD, error) {
	var ofd OFD
	return &ofd, ofd.Open(input)
}

// Open 打开OFD文件，支持文件路径、字节数据或通用 io.Reader。
// 对于 io.Reader，Open 会读取全部内容，但不会关闭传入的 reader。
func (p *OFD) Open(input any) error {
	switch v := input.(type) {
	case string:
		return p.openFromFile(v)
	case []byte:
		return p.openFromBytes(v)
	case io.Reader:
		data, err := io.ReadAll(v)
		if err != nil {
			return fmt.Errorf("读取OFD内容失败: %w", err)
		}
		return p.openFromBytes(data)
	default:
		return fmt.Errorf("不支持的类型: %T, 请提供文件路径(string)、文件数据([]byte)或 io.Reader", input)
	}
}

// openFromFile 从文件路径打开OFD文件
func (p *OFD) openFromFile(filePath string) error {
	cleanPath := filepath.Clean(filePath)
	if _, err := os.Stat(cleanPath); err != nil {
		return fmt.Errorf("文件路径验证失败: %w", err)
	}

	zr, err := zip.OpenReader(cleanPath)
	if err != nil {
		return fmt.Errorf("打开OFD文件失败: %w", err)
	}

	return p.openZipReader(&zr.Reader, zr)
}

// openFromBytes 从字节数据打开OFD文件
func (p *OFD) openFromBytes(data []byte) error {
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("从字节数据创建zip reader失败: %w", err)
	}

	return p.openZipReader(zipReader, nil)
}

// openZipReader 解析候选 ZIP，并在成功后一次性替换当前文档状态。
// 这样重复调用 Open 时，解析失败不会破坏当前已打开的文档。
func (p *OFD) openZipReader(zipReader *zip.Reader, reader *zip.ReadCloser) (err error) {
	if reader != nil {
		defer func() {
			if err != nil {
				_ = reader.Close()
			}
		}()
	}

	root, documents, fileCache, err := parseZipReader(zipReader)
	if err != nil {
		return err
	}
	if err = p.Close(); err != nil {
		return err
	}

	p.OFD = root
	p.Documents = documents
	p.fileCache = fileCache
	p.reader = reader
	return nil
}

// Close 关闭OFD解析器并释放资源
func (p *OFD) Close() error {
	var err error
	if p.reader != nil {
		err = p.reader.Close()
		p.reader = nil
	}
	p.fileCache = nil
	p.Documents = nil
	p.OFD = models.OFD{}
	if err != nil {
		return fmt.Errorf("关闭OFD文件失败: %w", err)
	}
	return nil
}

func parseZipReader(zipReader *zip.Reader) (models.OFD, []*Document, *core.ZipFileCache, error) {
	fileCache := core.NewZipFileCache(zipReader)
	var root models.OFD
	if err := fileCache.ParseXMLContent(rootDocument, &root); err != nil {
		return models.OFD{}, nil, nil, err
	}

	documents := make([]*Document, 0, len(root.DocBodies))
	for _, body := range root.DocBodies {
		document := &Document{}
		document.Init(fileCache, body.DocRoot)
		if err := document.parse(body); err != nil {
			return models.OFD{}, nil, nil, err
		}
		if err := document.ParseSigns(body.Signatures); err != nil {
			return models.OFD{}, nil, nil, err
		}
		documents = append(documents, document)
	}
	return root, documents, fileCache, nil
}
