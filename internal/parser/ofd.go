package parser

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/utils"
)

// OFD 表示一个OFD文档解析器
type OFD struct {
	models.OFD
	reader    *zip.ReadCloser
	fileCache *utils.ZipFileCache
	file      string

	Documents []*Document
}

const (
	rootDocument = "OFD.xml"
)

func NewOFD(file interface{}) (*OFD, error) {
	var ofd OFD
	return &ofd, ofd.Open(file)
}

// Open 打开OFD文件，支持文件路径或字节数据
func (p *OFD) Open(input interface{}) error {
	switch v := input.(type) {
	case string:
		return p.openFromFile(v)
	case []byte:
		return p.openFromBytes(v)
	default:
		return fmt.Errorf("不支持的类型: %T, 请提供文件路径(string)或文件数据([]byte)", input)
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

	defer func() {
		if err != nil {
			_ = zr.Close()
		}
	}()

	p.fileCache = utils.NewZipFileCache(&zr.Reader)
	// 查找根文档
	if err = p.fileCache.ParseXMLContent(rootDocument, &p.OFD); err != nil {
		return err
	}

	// 所有操作成功后才赋值
	p.file = cleanPath
	p.reader = zr

	return p.parse()
}

// openFromBytes 从字节数据打开OFD文件
func (p *OFD) openFromBytes(data []byte) error {
	// 创建zip.Reader
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("从字节数据创建zip reader失败: %w", err)
	}

	return p.openFromZipReader(zipReader)
}

// 从zip.Reader打开
func (p *OFD) openFromZipReader(zipReader *zip.Reader) error {
	p.fileCache = utils.NewZipFileCache(zipReader)
	// 查找根文档
	if err := p.fileCache.ParseXMLContent(rootDocument, &p.OFD); err != nil {
		return err
	}

	p.file = ""
	p.reader = nil

	return p.parse()
}

// Close 关闭OFD解析器并释放资源
func (p *OFD) Close() error {
	if p.reader != nil {
		if err := p.reader.Close(); err != nil {
			return fmt.Errorf("关闭OFD文件失败: %w", err)
		}
		p.reader = nil
		p.file = ""
	}
	return nil
}

func (p *OFD) parse() error {
	var err error
	if err = p.parseDocument(); err != nil {
		return err
	}

	return nil
}
func (p *OFD) parseDocument() error {
	var err error
	for _, body := range p.OFD.DocBodies {
		var document Document
		document.Init(p.fileCache, body.DocRoot)
		if err = document.parse(body); err != nil {
			return err
		}
		if err = document.ParseSigns(body.Signatures); err != nil {
			return err
		}

		p.Documents = append(p.Documents, &document)

	}

	return nil
}
