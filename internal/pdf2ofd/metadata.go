package pdf2ofd

import (
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

const (
	// converterName 写入 OFD DocInfo/Creator，标识转换工具本身。
	converterName = "zc310/ofd"
	// converterVersion 写入 OFD DocInfo/CreatorVersion。
	converterVersion = "0.0.1"
)

// pdfDocumentMetadata 生成 OFD 文档的创建者信息与自定义元数据。
// Creator/CreatorVersion 标识转换工具；源 PDF 的格式与生产者保留在
// CustomDatas，避免覆盖后丢失溯源信息。
func pdfDocumentMetadata(sourceProducer string) (name, version string, customDatas []creator.CustomData) {
	customDatas = make([]creator.CustomData, 0, 2)
	customDatas = append(customDatas, creator.CustomData{Name: "SourceFormat", Value: "PDF"})
	if producer := strings.TrimSpace(sourceProducer); producer != "" {
		customDatas = append(customDatas, creator.CustomData{Name: "SourceProducer", Value: producer})
	}
	return converterName, converterVersion, customDatas
}
