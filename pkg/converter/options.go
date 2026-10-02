package converter

import (
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/render/geom"
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
		c.dpi = geom.DPI(dpi)
	}
}

// PNG 设置为PNG格式
func PNG() Option { return WithFormat("png") }

// JPG 设置为JPEG格式
func JPG() Option { return WithFormat("jpeg") }

// ImageOCR 设置图片导入时是否启用 OCR。默认关闭。
func ImageOCR(enabled bool) Option {
	return func(c *Converter) { c.imageOCREnabled = enabled }
}

// ImageOCRLanguage 设置图片导入使用的 OCR 语言。
func ImageOCRLanguage(language string) Option {
	return func(c *Converter) { c.imageOCRLanguage = strings.TrimSpace(language) }
}

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

// WithHTMLTextLayer 设置 HTML 输出是否附带透明文字层，默认开启。
//
// 文字层让页面里的文字可选中、可被浏览器内查找、可被屏幕阅读器朗读，而视觉
// 仍由页面图像承担。它不内嵌字体，字号用容器查询单位随页面缩放，因此对
// 产物体积几乎没有影响。文字度量由浏览器用系统字体完成，与 OFD 版面可能有
// 细微偏差；关闭后回到纯图像输出。
func WithHTMLTextLayer(enabled bool) Option {
	return func(c *Converter) {
		c.htmlTextLayerOff = !enabled
	}
}

// WithDOCXTables 设置 OFD 转 DOCX 时是否识别并输出表格，默认开启。
// 识别复用 Markdown 的几何推断（X 对齐与空白间隔），而 DOCX 里表格带框线与
// 单元格边界，一旦误判比纯文字输出更难接受，因此默认开启、允许关闭。
func WithDOCXTables(enabled bool) Option {
	return func(c *Converter) {
		c.docxTablesOff = !enabled
	}
}

// WithDOCXImages 设置 OFD 转 DOCX 时是否内嵌图片，默认开启。
// 只内嵌 WordprocessingML 能直接承载的栅格格式；SVG 等矢量图片会被跳过，
// 因为转成位图会损失清晰度。每页最多内嵌 64 张，避免整版图片类的文档产出
// 体量失控的文件。
func WithDOCXImages(enabled bool) Option {
	return func(c *Converter) {
		c.docxImagesOff = !enabled
	}
}

// WithDOCXAnnotations 设置 OFD 转 DOCX 时是否保留批注层的文字，默认剔除。
// 批注承载的是叠加在正文上的标记——整页水印、电子印章、签章位置、阅读批注。
// 实测保密宣传册首页的「保密资料」水印由 81 个批注文字对象组成，同页真实正文
// 只有 5 个，不剔除时每个段落都会被水印文字淹没。需要保留叠加层时打开它。
func WithDOCXAnnotations(enabled bool) Option {
	return func(c *Converter) {
		c.docxAnnotations = enabled
	}
}

// WithPassword 设置加密输入文档的打开口令。
//
// 目前只作用于 PDF 输入：口令交给 pdfcpu 用于解密，OFD 的包级加密尚不支持。
// 空口令表示未提供，此时遇到加密文档会报"该文档已加密"，而不是给出一个
// 难以理解的解析失败。
func WithPassword(password string) Option {
	return func(c *Converter) {
		c.inputPassword = password
	}
}

// RasterBackend 指定 PNG/JPG 输出使用的栅格后端名（如 "canvas"、"gg"、
// "ftgg"、"tinyskia"）。仅对 PNG/JPEG 生效；空字符串表示使用 canvas 矢量
// 表面光栅化（默认）。使用 canvas 之外的后端需在程序中空白导入对应插件包，
// 例如 import _ "github.com/zc310/ofd/internal/render/backends/gg"。
func RasterBackend(name string) Option {
	return func(c *Converter) { c.rasterBackend = name }
}
