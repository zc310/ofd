// Package converter 提供 OFD 文档到 PDF、HTML、文本、Markdown 和图像等格式的转换能力。
package converter

import (
	"image"
	"image/color"
	"io"
	"time"

	"github.com/zc310/ofd/internal/render/geom"
)

// Converter 配置转换器
type Converter struct {
	dpi               geom.Resolution
	format            string // png, jpeg, svg, eps, tex
	rasterBackend     string // 非空时 PNG/JPG 走该栅格后端（BackendGG 等）
	htmlImageFormat   string // png, svg
	bgColor           color.Color
	page              int
	thumbnail         int
	pageCacheCapacity int
	pageCacheBytes    int64
	pdfParallel       bool
	imageWriter       func(page int, img image.Image) error
	fileWriter        func(page int) (io.WriteCloser, error)
	docTitle          string // 文档标题，由 HTML 等编码器使用
	markdownTables    bool   // Markdown 输出是否识别表格
	sofficePath       string
	officeTimeout     time.Duration
	chromePath        string
	paper             Paper
	printBackground   bool
	allowRemote       bool
	noSandbox         bool
	tempDir           string
}

// Option 配置选项类型
type Option func(*Converter)

// 默认配置
var defaultConverter = &Converter{
	dpi:             geom.DPI(300),
	format:          "png",
	htmlImageFormat: "png",
	bgColor:         color.Transparent,
	page:            0,
	thumbnail:       0,
}

// newConverter 创建转换器
func newConverter(options ...Option) *Converter {
	conv := &Converter{
		dpi:               defaultConverter.dpi,
		format:            defaultConverter.format,
		htmlImageFormat:   defaultConverter.htmlImageFormat,
		bgColor:           defaultConverter.bgColor,
		page:              defaultConverter.page,
		thumbnail:         defaultConverter.thumbnail,
		pageCacheCapacity: defaultConverter.pageCacheCapacity,
		pageCacheBytes:    defaultConverter.pageCacheBytes,
		pdfParallel:       false,
		paper:             DefaultPaper(),
		printBackground:   true,
	}

	for _, opt := range options {
		opt(conv)
	}
	return conv
}

// SofficePath 返回配置的 LibreOffice 可执行文件路径；为空表示自动查找。
func (c *Converter) SofficePath() string {
	if c == nil {
		return ""
	}
	return c.sofficePath
}

// OfficeTimeout 返回 Office 文档转换超时时间；为 0 表示使用默认值。
func (c *Converter) OfficeTimeout() time.Duration {
	if c == nil {
		return 0
	}
	return c.officeTimeout
}

// ChromePath 返回配置的 Chrome/Chromium 可执行文件路径；为空表示自动查找。
func (c *Converter) ChromePath() string {
	if c == nil {
		return ""
	}
	return c.chromePath
}

// Paper 返回输出纸张设置。
func (c *Converter) Paper() Paper {
	if c == nil {
		return DefaultPaper()
	}
	return c.paper
}

// PrintBackground 返回是否打印背景。
func (c *Converter) PrintBackground() bool {
	if c == nil {
		return true
	}
	return c.printBackground
}

// AllowRemoteResources 返回是否允许加载外部资源。
func (c *Converter) AllowRemoteResources() bool {
	if c == nil {
		return false
	}
	return c.allowRemote
}

// NoSandbox 返回是否禁用 Chrome 沙箱。
func (c *Converter) NoSandbox() bool {
	if c == nil {
		return false
	}
	return c.noSandbox
}

// TempDir 返回外部工具（LibreOffice/Chrome）转换使用的临时文件根目录；
// 为空表示使用系统临时目录。
func (c *Converter) TempDir() string {
	if c == nil {
		return ""
	}
	return c.tempDir
}

// MarkdownTables 返回 OFD 转 Markdown 时是否识别并输出表格，默认关闭。
func (c *Converter) MarkdownTables() bool {
	if c == nil {
		return false
	}
	return c.markdownTables
}
