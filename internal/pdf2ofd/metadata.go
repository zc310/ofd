package pdf2ofd

import (
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/creator"
)

// converterName 写入 OFD DocInfo/Creator，标识转换工具本身。
const converterName = "zc310/ofd"

// converterVersion 写入 OFD DocInfo/CreatorVersion，取自 internal/version，与
// --version 输出、报告里的 tool.version 同源。
//
// 此前写死 "0.0.1"，转换产物的元数据因此长期显示着一个早已不存在的版本。代价是
// 同一份 PDF 用不同版本转换会得到不同的 CreatorVersion——这本来就是事实，隐藏
// 它只会让溯源信息失真；需要逐字节可复现的输出用 `--deterministic`。
var converterVersion = version.Version

// pdfDocumentMetadata 生成 OFD 文档的创建者信息与自定义元数据。
// Creator/CreatorVersion 标识转换工具；源 PDF 的格式与生产者保留在
// CustomDatas，避免覆盖后丢失溯源信息。
//
// 具名返回值不叫 version：那会遮蔽同名的 internal/version 包，读者无法分辨这个
// 版本号从哪来。
func pdfDocumentMetadata(sourceProducer string) (name, toolVersion string, customDatas []creator.CustomData) {
	customDatas = make([]creator.CustomData, 0, 2)
	customDatas = append(customDatas, creator.CustomData{Name: "SourceFormat", Value: "PDF"})
	if producer := strings.TrimSpace(sourceProducer); producer != "" {
		customDatas = append(customDatas, creator.CustomData{Name: "SourceProducer", Value: producer})
	}
	return converterName, converterVersion, customDatas
}

// pdfDocumentDates 读取 PDF /Info 的创建与修改时间。
//
// 日期字符串形如 D:YYYYMMDDHHmmSSOHH'mm'，缺项或格式异常时返回零值，由创建器
// 按自己的规则兜底。转换出的链接注解用文档日期填 LastModDate，文档没有日期时
// 它们会全部落到 1970-01-01。
func pdfDocumentDates(ctx *model.Context) (created, modified time.Time) {
	if ctx == nil || ctx.XRefTable == nil {
		return created, modified
	}
	if value, ok := types.DateTime(ctx.XRefTable.CreationDate, true); ok {
		created = value
	}
	if value, ok := types.DateTime(ctx.XRefTable.ModDate, true); ok {
		modified = value
	}
	return created, modified
}
