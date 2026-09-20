package converter

import (
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"github.com/tdewolff/canvas"
)

// WithFormat 按注册的格式名（或别名）设置输出格式。
func WithFormat(format string) Option {
	return func(c *Converter) {
		c.format = strings.ToLower(strings.TrimSpace(format))
	}
}

// Thumbnail 设置缩略图大小
func Thumbnail(s int) Option {
	return func(c *Converter) {
		c.thumbnail = s
	}
}

// Writer 设置文件写入器
func Writer(f func(page int) (io.WriteCloser, error)) Option {
	return func(c *Converter) {
		c.fileWriter = f
	}
}

// ImageWriter 设置图像写入器
func ImageWriter(f func(page int, img image.Image) error) Option {
	return func(c *Converter) {
		c.imageWriter = f
	}
}

// DPI 设置DPI
func DPI(dpi float64) Option {
	return func(c *Converter) {
		c.dpi = canvas.DPI(dpi)
	}
}

// PNG 设置为PNG格式
func PNG() Option { return WithFormat("png") }

// JPG 设置为JPEG格式
func JPG() Option { return WithFormat("jpeg") }

// SVG 设置为 SVG 格式
func SVG() Option { return WithFormat("svg") }

// HTMLPNG 设置 HTML 页面使用内嵌 PNG 图片。
func HTMLPNG() Option {
	return func(c *Converter) {
		c.htmlImageFormat = "png"
	}
}

// HTMLJPG 设置 HTML 页面使用内嵌 JPG 图片。
func HTMLJPG() Option {
	return func(c *Converter) {
		c.htmlImageFormat = "jpg"
	}
}

// HTMLSVG 设置 HTML 页面使用内嵌 SVG 内容。
func HTMLSVG() Option {
	return func(c *Converter) {
		c.htmlImageFormat = "svg"
	}
}

// EPS 设置为 Encapsulated PostScript 格式
func EPS() Option { return WithFormat("eps") }

// TeX 设置为 TeX/PGF 格式
func TeX() Option { return WithFormat("tex") }

// BgColor 设置背景颜色
func BgColor(bg color.Color) Option {
	return func(c *Converter) {
		c.bgColor = bg
	}
}

// PageCache 设置页面缓存的最大页数和估算最大字节数；0 表示使用解析器默认值。
func PageCache(capacity int, maxBytes int64) Option {
	return func(c *Converter) {
		c.pageCacheCapacity = capacity
		c.pageCacheBytes = maxBytes
	}
}

// Page 设置全局页码，从 1 开始；0 表示处理全部页面。
func Page(page int) Option {
	return func(c *Converter) {
		c.page = page
	}
}

// PDFParallel 控制 PDF 页面是否并行渲染；默认关闭。
func PDFParallel(enabled bool) Option {
	return func(c *Converter) {
		c.pdfParallel = enabled
	}
}

// WithSoffice 设置 LibreOffice 可执行文件路径，供 Office 文档导入使用；
// 为空时按 OFD_SOFFICE、PATH 和常见安装路径自动查找。
func WithSoffice(path string) Option {
	return func(c *Converter) {
		c.sofficePath = strings.TrimSpace(path)
	}
}

// WithOfficeTimeout 设置 Office 文档转换超时时间；0 表示使用默认值。
func WithOfficeTimeout(timeout time.Duration) Option {
	return func(c *Converter) {
		c.officeTimeout = timeout
	}
}

// WithPaperSize 按名称设置输出纸张尺寸，支持 A4、A3、A5、Letter、Legal、B5、16开。
func WithPaperSize(name string) Option {
	return func(c *Converter) {
		paper, err := PaperByName(name)
		if err != nil {
			return
		}
		c.paper.Width = paper.Width
		c.paper.Height = paper.Height
	}
}

// WithPaperDimensions 以毫米设置自定义纸张尺寸。
func WithPaperDimensions(width, height float64) Option {
	return func(c *Converter) {
		if width > 0 && height > 0 {
			c.paper.Width = width
			c.paper.Height = height
		}
	}
}

// WithLandscape 设置横向打印。
func WithLandscape(landscape bool) Option {
	return func(c *Converter) {
		c.paper.Landscape = landscape
	}
}

// WithPrintBackground 设置是否打印背景颜色和图片（默认开启）。
func WithPrintBackground(enabled bool) Option {
	return func(c *Converter) {
		c.printBackground = enabled
	}
}

// WithAllowRemoteResources 设置是否允许加载外部资源（默认禁止，仅允许本地与
// data: 资源）。仅在 HTML/MHTML 转 PDF 时生效。
func WithAllowRemoteResources(enabled bool) Option {
	return func(c *Converter) {
		c.allowRemote = enabled
	}
}

// WithChrome 设置 Chrome/Chromium 可执行文件路径，供 HTML/MHTML 导入使用；
// 为空时按 OFD_CHROME、PATH 和常见安装路径自动查找。
func WithChrome(path string) Option {
	return func(c *Converter) {
		c.chromePath = strings.TrimSpace(path)
	}
}

// WithChromeNoSandbox 设置是否禁用 Chrome 沙箱。容器或 root 环境下可能需要，
// 但会降低安全性，默认关闭。
func WithChromeNoSandbox(enabled bool) Option {
	return func(c *Converter) {
		c.noSandbox = enabled
	}
}

// WithTempDir 设置外部工具（LibreOffice/Chrome）转换使用的临时文件根目录；
// 为空表示使用系统临时目录。目录不存在时会按需创建子目录。
func WithTempDir(dir string) Option {
	return func(c *Converter) {
		c.tempDir = strings.TrimSpace(dir)
	}
}

// WithMarkdownTables 设置 OFD 转 Markdown 时是否识别并输出表格，默认关闭。
// 表格识别基于文字位置（X 对齐与空白间隔），对无边框表格有效，但双栏正文、
// 公式排版等也可能被误判，因此默认关闭，按需开启。
func WithMarkdownTables(enabled bool) Option {
	return func(c *Converter) {
		c.markdownTables = enabled
	}
}
