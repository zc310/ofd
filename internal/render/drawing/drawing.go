// Package drawing 定义与具体绘制库无关的绘制后端契约、注册表，以及 render
// 核心与各实现包共享的公共契约类型（字体、矢量输出、SVG 场景等）。
//
// render 的页面绘制代码面向 DrawContext；canvas 内置后端与各插件后端
// （gg/ftgg/tinyskia/draw2d）都实现它。后端插件通过 RegisterBackend 注册后，
// 由 render.NewBackend 按名创建。把契约放在本包，使插件包只需依赖 geom 与
// 本包，不必依赖 render（从而不连带引入 tdewolff/canvas）。
package drawing

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// 后端标识，供 render.NewBackend / Document.RasterizePage 使用。
const (
	// BackendCanvas 使用 tdewolff/canvas 光栅器（render 内置默认路径）。
	BackendCanvas = "canvas"
	// BackendGG 使用 gogpu/gg 光栅后端。需要空白导入
	// github.com/zc310/ofd/internal/render/backends/gg 才能注册使用。
	BackendGG = "gg"
	// BackendFTGG 使用 FloatTech/gg 光栅后端（测速对照）。
	BackendFTGG = "ftgg"
	// BackendFGG 使用 fogleman/gg 光栅后端（测速对照，FloatTech/gg 的
	// 上游原版）。需要空白导入
	// github.com/zc310/ofd/internal/render/backends/fgg 才能注册使用。
	BackendFGG = "fgg"
	// BackendTinySkia 使用 tinyskia 光栅后端（测速对照）。
	BackendTinySkia = "tinyskia"
	// BackendDraw2D 使用 llgcode/draw2d 光栅后端（测速对照）。需要空白导入
	// github.com/zc310/ofd/internal/render/backends/draw2d 才能注册使用。
	BackendDraw2D = "draw2d"
)

// DrawContext 是 OFD 页面绘制所依赖的最小操作面，全部使用与绘制库无关的
// geom 类型：canvas 后端负责把 geom 转成 canvas 类型，gg/ftgg/tinyskia
// 后端直接消费 geom 的段数据与采样渐变。
type DrawContext interface {
	// Push/Pop 保存并恢复当前样式与变换。
	Push()
	Pop()

	// Translate/Scale/Rotate 修改当前变换（文档坐标 → 设备坐标）。
	Translate(x, y float64)
	Scale(sx, sy float64)
	Rotate(deg float64)

	// CurrentMatrix 返回当前完整矩阵（用于把预渲染的离屏图按正确位置贴回）。
	CurrentMatrix() geom.Matrix

	// SetFillColor 设置纯色填充。SetFillGradient 设置渐变填充。
	SetFillColor(c color.Color)
	SetFillGradient(g geom.Gradient)

	// SetFillPaint 设置填充画笔（颜色或渐变）。
	SetFillPaint(paint geom.Paint)

	// ClearFill 清除填充。
	ClearFill()

	// SetStrokeColor 设置纯色描边。SetStrokeGradient 设置描边渐变。
	SetStrokeColor(c color.Color)
	SetStrokeGradient(g geom.Gradient)

	// SetStrokePaint 设置描边画笔（颜色或渐变）。
	SetStrokePaint(paint geom.Paint)

	// ClearStroke 清除描边。
	ClearStroke()

	// CopyStrokeToFill 把当前描边样式复制为填充样式。
	CopyStrokeToFill()

	// SetStrokeWidth 设置描边宽度。SetDashes 设置虚线数组。
	SetStrokeWidth(w float64)
	SetDashes(offset float64, dashes ...float64)
	SetStrokeCapper(cap geom.Capper)
	SetStrokeJoiner(join geom.Joiner)

	// StrokeWidth/StrokeCapper/StrokeJoiner 读回当前描边样式。
	StrokeWidth() float64
	StrokeCapper() geom.Capper
	StrokeJoiner() geom.Joiner

	// SetFillRule 设置填充规则（geom.NonZero / geom.EvenOdd）。
	SetFillRule(rule geom.FillRule)

	// DrawPath 按当前样式绘制路径（x,y 为路径的平移到画布的偏移）。
	DrawPath(x, y float64, p *geom.Path)

	// TextPath 绘制字形轮廓路径（baseline 位于 (0,0)，后端平移到 (x,y)）。
	TextPath(p *geom.Path, x, y float64)

	// DrawImage 绘制图片：dpmm 是源图像素到画布单位（mm）的换算。
	DrawImage(img image.Image, x, y float64, dpmm float64)

	// RenderImage 按绝对设备矩阵绘制预渲染图片。
	RenderImage(img image.Image, m geom.Matrix)
}

// Backend 是可切换的页面栅格化后端：以 DrawContext 接口接收 OFD 页面
// 绘制指令，绘制结束后把结果取回为标准 RGBA 图像。
type Backend interface {
	DrawContext
	// Raster 返回当前后端绘制的全部内容对应的 RGBA 图像。
	Raster() *image.RGBA
}

// BackendFactory 创建后端实例，由各后端插件包通过 RegisterBackend 注册。
// width/height 为页面物理尺寸（mm），resolution 为输出分辨率。
type BackendFactory func(width, height float64, resolution geom.Resolution) (Backend, error)

var (
	backendMu     sync.RWMutex
	backendByName = map[string]BackendFactory{}
)

// RegisterBackend 注册一个可按名字创建的渲染后端，供 render.NewBackend 使用。
// 插件包在自己的 init() 里调用本函数。重复注册同名后端返回错误。
func RegisterBackend(name string, factory BackendFactory) error {
	if name == "" {
		return errors.New("渲染后端名称不能为空")
	}
	if factory == nil {
		return fmt.Errorf("渲染后端 %q 的工厂函数为空", name)
	}
	backendMu.Lock()
	defer backendMu.Unlock()
	if _, dup := backendByName[name]; dup {
		return fmt.Errorf("渲染后端 %q 已注册", name)
	}
	backendByName[name] = factory
	return nil
}

// RegisterOrReplaceBackend 注册后端；若同名已注册则替换。供“用另一实现提供
// 同名后端”的插件使用（例如用另一实现覆盖已有的 "gg"）。
func RegisterOrReplaceBackend(name string, factory BackendFactory) error {
	if name == "" {
		return errors.New("渲染后端名称不能为空")
	}
	if factory == nil {
		return fmt.Errorf("渲染后端 %q 的工厂函数为空", name)
	}
	backendMu.Lock()
	defer backendMu.Unlock()
	backendByName[name] = factory
	return nil
}

// LookupBackend 返回已注册的后端工厂与是否存在的标志。
func LookupBackend(name string) (BackendFactory, bool) {
	backendMu.RLock()
	defer backendMu.RUnlock()
	factory, ok := backendByName[name]
	return factory, ok
}

// RegisteredBackends 返回当前已注册的可选后端名称（不含始终内置的 canvas），
// 按名称排序，主要用于错误提示。
func RegisteredBackends() []string {
	backendMu.RLock()
	defer backendMu.RUnlock()
	names := make([]string, 0, len(backendByName))
	for name := range backendByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BackendNames 返回内置 canvas 与全部已注册后端名称，用于错误提示。
func BackendNames() []string { return append([]string{BackendCanvas}, RegisteredBackends()...) }

// BackendPluginPath 返回后端名对应的插件包路径片段。
func BackendPluginPath(name string) string {
	return "github.com/zc310/ofd/internal/render/backends/" + name
}

// JoinNames 用「、」连接后端名列表。
func JoinNames(names []string) string { return strings.Join(names, "、") }

// 共享的公共契约类型：字体、矢量输出、SVG 场景等，与后端契约同处本包。

// CTColor 是解析后的绘制颜色：固定值颜色（HasValue）或渐变。Value 用值类型
// 内联，避免每次 updateCtColor 都堆分配 *color.RGBA 与 *CTColor。
type CTColor struct {
	Value    color.RGBA
	HasValue bool
	Gradient geom.Gradient
}

// FontFamily 是已解析的字体族句柄。具体字体引擎实现它；核心绘制代码只通过
// 该句柄取字体面，不依赖具体引擎类型。
type FontFamily interface {
	Name() string
}

// FontFace 是一次字号与样式确定的字体面，供文字度量与字形轮廓绘制使用。
type FontFace interface {
	// ToPath 返回按字体整形后的单行文本轮廓路径（y 向上字号坐标系）。
	ToPath(value string) *geom.Path
	// DirectPath 返回按字形 ID 直接映射的轮廓路径；无法映射时返回 nil。
	DirectPath(value string) *geom.Path
	// TextWidth 返回文本的排版宽度（毫米）。
	TextWidth(value string) float64
	// GlyphIndex 返回 Unicode 码位对应的字形 ID；缺失返回 0。
	GlyphIndex(r rune) uint16
	// IsCFF 判断底层字体是否为 CFF 轮廓。
	IsCFF() bool
	// Fill 返回字体面携带的画笔（颜色或渐变）。
	Fill() geom.Paint
	// Size 返回字号（点）。
	Size() float64
	// ShapedRun 返回引擎整形的单行文本；不支持时返回 nil。
	ShapedRun(value string) TextRun
}

// TextRun 是字体引擎整形的单行文本，对核心不透明。
type TextRun interface{}

// FontEngine 是文档字体解析与字体面创建的引擎接口。
type FontEngine interface {
	// LoadFont 解析指定字体资源并返回字体族句柄。
	LoadFont(id models.StRefID) (FontFamily, error)
	// FaceObject 为字体族与文字对象创建字体面。
	FaceObject(family FontFamily, object models.TextObject, fill *CTColor) FontFace
	// RenderLock 返回串行化该字体族渲染的互斥锁。
	RenderLock(family FontFamily) *sync.Mutex
	// RegisterPageGlyphs 扫描页面文字并登记 Unicode→字形映射。
	RegisterPageGlyphs(doc *parser.Document, page *parser.Page, content *models.PageContent)
	// UseFallbackFont 使引擎在缺失字体时使用已全局注册的回退字体族。
	UseFallbackFont(family string) error
	// FallbackFontFamily 返回为文档字体选择的外部字体族。
	FallbackFontFamily(id models.StRefID) string
	// HasLoadedEmbeddedFont 判断文档字体是否解析为可用且已加载的内嵌字体。
	HasLoadedEmbeddedFont(id models.StRefID) bool
}

// VectorSurface 是页面绘制得到的矢量表面：既可按分辨率栅格化为位图，也可
// 序列化为 SVG/EPS/TeX 等矢量格式。
type VectorSurface interface {
	// Width/Height 返回页面物理尺寸（mm）。
	Width() float64
	Height() float64
	// Write 以 format 指定的矢量格式写出单页内容，支持 "svg"/"eps"/"tex"。
	Write(w io.Writer, format string) error
	// Rasterize 按分辨率把页面栅格化为标准 RGBA。
	Rasterize(resolution geom.Resolution) *image.RGBA
}

// PDFOptions 控制 PDF 文档输出。
type PDFOptions struct {
	Compress    bool // 压缩内容流
	SubsetFonts bool // 子集化 TrueType 字体
	LossyImages bool // 图片使用有损编码
}

// PDFDocument 是中性的 PDF 多页文档写入器：按页序逐页加入矢量表面。
type PDFDocument interface {
	AddPage(page VectorSurface) error
	Close() error
}

// SVGScene 是与绘制库无关的已解析 SVG 场景。
type SVGScene interface {
	// Width/Height 返回 SVG 的原始画布尺寸（用户单位）。
	Width() float64
	Height() float64
	// Rasterize 按分辨率栅格化为标准图像。
	Rasterize(resolution geom.Resolution) image.Image
	// RenderVector 尝试以矢量方式嵌入当前绘制上下文，返回是否完成。
	RenderVector(ctx any, m geom.Matrix) bool
}

// FontStyle 是与具体字体引擎无关的字重/斜体样式，取值与 canvas.FontStyle
// 保持一致，便于无损转换。各引擎实现把它映射到自己的样式表示。
type FontStyle int

// 字重常量（与 canvas 一致）与斜体位。
const (
	FontRegular FontStyle = iota
	FontThin
	FontExtraLight
	FontLight
	FontMedium
	FontSemiBold
	FontBold
	FontExtraBold
	FontBlack
	FontItalic FontStyle = 1 << 8
)

// Italic 判断是否斜体。
func (s FontStyle) Italic() bool { return s&FontItalic != 0 }

// Weight 返回字重部分。
func (s FontStyle) Weight() FontStyle { return s & 0xFF }

// CSS 返回对应的 CSS 字重值。
func (s FontStyle) CSS() int {
	switch s.Weight() {
	case FontThin:
		return 100
	case FontExtraLight:
		return 200
	case FontLight:
		return 300
	case FontMedium:
		return 500
	case FontSemiBold:
		return 600
	case FontBold:
		return 700
	case FontExtraBold:
		return 800
	case FontBlack:
		return 900
	default:
		return 400
	}
}
