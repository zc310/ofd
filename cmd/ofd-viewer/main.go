// 命令 ofd-viewer 提供 OFD 文档的图形化查看和导出功能。
package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zip"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fyneCanvas "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/utils"
	canvasConverter "github.com/zc310/ofd/pkg/converter"
)

// errDocumentChanged 表示导出期间文档被替换或关闭，导出没有完成。
var errDocumentChanged = errors.New("文档已更改")

const (
	applicationID = "io.github.zc310.ofd"
	projectURL    = "https://github.com/zc310/ofd"
	windowWidth   = 800
	windowHeight  = 600
	viewerDPI     = 96
	thumbnailDPI  = 36
)

// applicationVersion 由构建系统通过 -ldflags "-X main.applicationVersion=..."
// 注入，Makefile 用单一 VERSION 变量同时驱动这里和 Android APK 的版本号。
var applicationVersion = "v" + defaultVersion

// defaultVersion 是未注入时的兜底版本号，与 Makefile 的 VERSION 保持一致。
const defaultVersion = "0.0.5"

const (
	exportFormatPDF = ".pdf（Portable Document Format）"
	exportFormatTXT = ".txt（纯文本）"
	exportFormatJPG = ".jpg（JPEG 图片）"
	exportFormatPNG = ".png（Portable Network Graphics）"
	exportFormatSVG = ".svg（Scalable Vector Graphics）"
	exportFormatEPS = ".eps（Encapsulated PostScript）"
	exportFormatTeX = ".tex（TeX/PGF）"
)

const (
	exportBackgroundTransparent = "透明"
	exportBackgroundWhite       = "白色"
)

const (
	exportDPI    = 150
	exportMinDPI = 1
	exportMaxDPI = 1200
	// exportPageBudget 限制导出单页栅格图像的内存占用。A4 页面在该预算下
	// 约可导出 590 DPI，足够放大阅读，又不会让单页 RGBA 达到数百 MB。
	exportPageBudget = 128 << 20
	// pageImageBudget 限制阅读区页面图像的常驻内存。96 DPI 的 A4 页面约
	// 3.5 MB，因此该预算大约保留 36 页，超出后淘汰视口外的页面。
	pageImageBudget  = 128 << 20
	pageImageEntries = 256
	// thumbnailBudget 限制缩略图的常驻内存。36 DPI 的 A4 页面约 0.5 MB，
	// 该预算大约保留 128 页缩略图。
	thumbnailBudget  = 64 << 20
	thumbnailEntries = 512
)

// initialPageFrames 是显示帧的初始数量。常见页面在视口内只显示一两页，
// 页面异常扁平时 bindPageWindow 会按需扩池。
const initialPageFrames = 12

// pagePrefetch 是显示窗口在可视区间两侧额外预留的页数。滚动通常先移动几
// 个像素再触发渲染，预留相邻页可以避免换页瞬间的白屏；多出来的位图仍然由
// pageImages 的字节预算兜底。
const pagePrefetch = 1

// pageLoadingTimeout 是页面加载提示的兜底超时。大页面渲染可能很慢，但提示
// 不能无限期遮挡界面；超时只隐藏提示，渲染完成后页面仍会正常显示。
// 声明为变量是为了测试可以缩短等待时间。
var pageLoadingTimeout = 15 * time.Second

func main() {
	var initialFile string
	if len(os.Args) > 1 {
		initialFile = validOFDFile(os.Args[1])
	}

	a := app.NewWithID(applicationID)
	w := a.NewWindow(applicationTitle())
	w.Resize(fyne.NewSize(windowWidth, windowHeight))
	w.CenterOnScreen()
	w.SetMaster()

	viewer := newViewer(w)
	w.SetContent(viewer.content)
	w.Canvas().SetOnTypedKey(viewer.handleKey)
	w.SetOnClosed(viewer.close)
	w.Show()

	if initialFile != "" {
		viewer.load(initialFile, filepath.Base(initialFile), initialFile)
	}
	a.Run()
}

type viewer struct {
	window fyne.Window

	content         *fyne.Container
	pageLayout      *continuousLayout
	pageContent     *fyne.Container
	pageScroll      *container.Scroll
	pageSlots       []*pageMeta
	thumbnailList   *widget.List
	openButton      *widget.Button
	menuButton      *widget.Button
	documentTitle   *widget.Label
	pageToolbar     *fyne.Container
	pageLabel       *widget.Label
	pageEntry       *widget.Entry
	thumbnailToggle *widget.Button
	pageLoading     *widget.PopUp
	pageLoadingOp   uint64
	pageLoadingSeq  atomic.Uint64
	pageLoadingStop *time.Timer
	exportLoading   *widget.PopUp
	thumbnailPanel  *fyne.Container
	documentArea    *container.Split

	// 除 atomic 字段外，viewer 的状态只在 Fyne 事件线程读写；后台任务只捕获
	// 已解析的文档和页面指针，不读取这些字段。栅格化与导出都不在事件线程上
	// 执行，也不持有任何跨调用锁，因此打开和关闭文档不会阻塞界面。
	//
	// pageImages 和 thumbnails 都由 LRU 按字节预算限流，访问同样只在 Fyne
	// 事件线程发生，因此淘汰回调可以直接更新槽位而不需要 fyne.Do。
	filePath            string
	fileName            string
	ofd                 *parser.OFD
	session             *documentSession
	documents           []*render.Document
	pages               []viewerPage
	maxPageArea         float64
	pageImages          *utils.LRU[int, *image.RGBA]
	thumbnails          *utils.LRU[int, *image.RGBA]
	currentPage         int
	totalPages          int
	loading             bool
	exporting           bool
	operation           atomic.Uint64
	thumbnailGeneration atomic.Uint64
	// syncingThumbnail 标记选中高亮由程序设置，避免 OnSelected 与选中同步
	// 互相触发形成回环。
	syncingThumbnail atomic.Bool
	// thumbnailSelected 记录当前高亮的缩略图行号。widget.List 不导出选中状态，
	// 同步高亮时需要自己比较，避免重复 Select。
	thumbnailSelected  int
	thumbnailRendering []atomic.Bool
	closed             atomic.Bool
	backDeadline       time.Time
}

type pageViewMode int

const (
	viewFitPage pageViewMode = iota
	viewFitWidth
	viewFitHeight
	viewDoublePage
)

const (
	viewFitPageLabel    = "适应页面"
	viewFitWidthLabel   = "适应宽度"
	viewFitHeightLabel  = "适应高度"
	viewDoublePageLabel = "双页显示"
)

// pageMeta 是与显示无关的页面元数据。布局虚拟化后不再为每个页面常驻一组
// Fyne 对象，因此这里只保留排版和按需渲染需要的标量。
type pageMeta struct {
	aspect     float32
	area       float64
	renderable bool
	rendering  atomic.Bool
}

// pageFrame 是可复用的页面显示对象。帧的数量只取决于视口能同时容纳多少页面，
// 与文档页数无关，因此上万页的文档也只常驻十几个帧。
type pageFrame struct {
	frame *fyne.Container
	image *fyneCanvas.Image
	// page 是当前绑定的全局页码，-1 表示空闲。
	page int
}

// pageSlotState 描述页面排版参数，与 Fyne 对象无关，便于回归测试。
type pageSlotState struct {
	// aspect 是页面显示宽高比；页面区域无法解析时回退为 1。
	aspect float32
	// area 是页面物理面积（平方毫米），用于换算导出时可用的最大 DPI。
	area float64
	// renderable 表示页面区域可读取；为 false 的槽位只占位，不参与按需渲染。
	renderable bool
}

// rasterWeight 返回栅格图像的内存占用，用于缓存的字节预算。
func rasterWeight(raster *image.RGBA) int64 {
	if raster == nil || len(raster.Pix) == 0 {
		return 1
	}
	return int64(len(raster.Pix))
}

// maxExportDPI 返回页面在导出内存预算内可用的最大 DPI。
// areaMM2 是页面物理面积（平方毫米）；areaMM2 未知时不额外限制。
func maxExportDPI(areaMM2 float64) int {
	if areaMM2 <= 0 {
		return exportMaxDPI
	}
	// RasterizePage 输出 RGBA，每像素 4 字节；geom.DPI(1) 是 1 DPI 对应的
	// 点/毫米数，用它换算可以避免重复定义毫米与英寸的比例。
	unit := float64(geom.DPI(1))
	limit := math.Sqrt(float64(exportPageBudget) / (4 * areaMM2 * unit * unit))
	if limit >= float64(exportMaxDPI) {
		return exportMaxDPI
	}
	if limit < float64(exportMinDPI) {
		return exportMinDPI
	}
	return int(math.Floor(limit))
}

// viewerPage 将全局页码映射到所属文档体中的页面，同时保留独立资源上下文。
type viewerPage struct {
	document *render.Document
	page     *parser.Page
}

type pageBound struct {
	position fyne.Position
	size     fyne.Size
}

type continuousLayout struct {
	mode       pageViewMode
	pages      []*pageMeta
	frames     []*pageFrame
	pageBounds []pageBound
	pageSizes  []fyne.Size
	pageScroll *container.Scroll
	gap        float32
	margin     float32
	minSize    fyne.Size
	viewport   fyne.Size
	// geometry 记录当前 pageBounds 对应的几何条件。页面排版只依赖视口、
	// 视图模式和页数，与滚动位置无关，因此几何不变时可以整段复用。
	geometry layoutGeometry
	// lastNotifiedViewport 记录上次通知外部时的视口尺寸，用来只在尺寸真正
	// 变化时触发一次，避免重复渲染可见页面。
	lastNotifiedViewport fyne.Size
	// onViewportChange 在视口尺寸变化、页面重新排版后调用。滚动事件之外的
	// 视口变化（例如拖动窗口大小）只能在这里被感知，否则新露出的页面不会被渲染。
	onViewportChange func()
}

// layoutGeometry 是页面排版结果的缓存键。
type layoutGeometry struct {
	viewport fyne.Size
	mode     pageViewMode
	pages    int
}

// resizeSlice 复用缓冲区并把长度调整为 size。长文档下每次排版都重新分配
// 页面尺寸和边界数组会带来明显开销。
func resizeSlice[T any](items []T, size int) []T {
	if cap(items) < size {
		return make([]T, size)
	}
	return items[:size]
}

// setPages 切换到新的页面集合并重建显示帧。帧数量与页数无关，因此换文档
// 不会带来额外开销。
func (l *continuousLayout) setPages(pages []*pageMeta) {
	l.pages = pages
	l.frames = nil
	if len(pages) > 0 {
		l.growFrames(initialPageFrames)
	}
	l.pageBounds = nil
	l.pageSizes = nil
	l.minSize = fyne.Size{}
	l.geometry = layoutGeometry{}
	l.viewport = fyne.Size{}
	l.lastNotifiedViewport = fyne.Size{}
}

func (l *continuousLayout) growFrames(count int) {
	for len(l.frames) < count {
		image := fyneCanvas.NewImageFromImage(nil)
		image.FillMode = fyneCanvas.ImageFillContain
		image.ScaleMode = fyneCanvas.ImageScaleSmooth
		l.frames = append(l.frames, &pageFrame{
			frame: container.NewStack(fyneCanvas.NewRectangle(color.White), image),
			image: image,
			page:  -1,
		})
	}
}

// frameObjects 返回与 frames 一一对应的画布对象，供页面容器持有。
func (l *continuousLayout) frameObjects() []fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, len(l.frames))
	for i, frame := range l.frames {
		objects[i] = frame.frame
	}
	return objects
}

func (l *continuousLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	if len(l.frames) == 0 {
		return
	}
	viewport := l.resolveViewport(size)
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}
	l.ensureGeometry(viewport)
	l.placeFrames()
	l.notifyViewportChange()
}

// ensureGeometry 在视口、视图模式或页数变化时重算排版结果。这是唯一与页数
// 成正比的步骤，只在几何真正变化时执行；滚动不会触发。
func (l *continuousLayout) ensureGeometry(viewport fyne.Size) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}
	key := layoutGeometry{viewport: viewport, mode: l.mode, pages: len(l.pages)}
	if key == l.geometry {
		return
	}
	l.rebuildGeometry(viewport, key)
}

// resolveViewport 返回页面排版依据的视口尺寸：优先用滚动容器的尺寸，容器
// 还没布局时退回调用方给出的内容尺寸。
func (l *continuousLayout) resolveViewport(size fyne.Size) fyne.Size {
	if l.pageScroll != nil {
		if viewport := l.pageScroll.Size(); viewport.Width > 0 && viewport.Height > 0 {
			return viewport
		}
	}
	return size
}

// rebuildGeometry 重算全部页面的尺寸和排版位置。这是唯一与页数成正比的步骤，
// 只在视口、视图模式或页数变化时执行；滚动不会触发。
func (l *continuousLayout) rebuildGeometry(viewport fyne.Size, key layoutGeometry) {
	count := len(l.pages)
	l.geometry = key
	l.viewport = viewport
	if count == 0 {
		l.pageBounds = l.pageBounds[:0]
		l.pageSizes = l.pageSizes[:0]
		l.minSize = fyne.Size{}
		return
	}
	pageSizes := resizeSlice(l.pageSizes, count)
	l.pageSizes = pageSizes
	l.pageBounds = resizeSlice(l.pageBounds, count)

	contentWidth := viewport.Width
	if l.mode == viewDoublePage {
		contentHeight := l.margin
		for i := 0; i < count; i++ {
			pageSizes[i] = l.pageSize(l.pageAspect(i), viewport)
		}
		for rowStart := 0; rowStart < count; rowStart += 2 {
			rowHeight := pageSizes[rowStart].Height
			if rowStart+1 < count && pageSizes[rowStart+1].Height > rowHeight {
				rowHeight = pageSizes[rowStart+1].Height
			}
			contentHeight += rowHeight
			if rowStart+2 < count {
				contentHeight += l.gap
			}
		}
		contentHeight += l.margin
		l.minSize = fyne.NewSize(contentWidth, contentHeight)

		y := l.margin
		for rowStart := 0; rowStart < count; rowStart += 2 {
			rowEnd := rowStart + 2
			if rowEnd > count {
				rowEnd = count
			}
			rowHeight := pageSizes[rowStart].Height
			if rowEnd-rowStart == 2 && pageSizes[rowStart+1].Height > rowHeight {
				rowHeight = pageSizes[rowStart+1].Height
			}
			rowWidth := pageSizes[rowStart].Width
			if rowEnd-rowStart == 2 {
				rowWidth += l.gap + pageSizes[rowStart+1].Width
			}
			x := (contentWidth - rowWidth) / 2
			for i := rowStart; i < rowEnd; i++ {
				pagePosition := fyne.NewPos(x, y+(rowHeight-pageSizes[i].Height)/2)
				l.pageBounds[i] = pageBound{position: pagePosition, size: pageSizes[i]}
				x += pageSizes[i].Width + l.gap
			}
			y += rowHeight + l.gap
		}
		return
	}

	contentHeight := l.margin
	for i := 0; i < count; i++ {
		pageSizes[i] = l.pageSize(l.pageAspect(i), viewport)
		if pageSizes[i].Width+2*l.margin > contentWidth {
			contentWidth = pageSizes[i].Width + 2*l.margin
		}
		contentHeight += pageSizes[i].Height
		if i < count-1 {
			contentHeight += l.gap
		}
	}
	contentHeight += l.margin
	l.minSize = fyne.NewSize(contentWidth, contentHeight)

	y := l.margin
	for i := 0; i < count; i++ {
		size := pageSizes[i]
		x := (contentWidth - size.Width) / 2
		l.pageBounds[i] = pageBound{
			position: fyne.NewPos(x, y),
			size:     size,
		}
		y += size.Height + l.gap
	}
}

// placeFrames 按当前绑定关系摆放显示帧。帧数量与页数无关，因此这一步与
// 文档长度无关；Move 和 Resize 在目标未变时由 Fyne 自行跳过。
func (l *continuousLayout) placeFrames() {
	for _, frame := range l.frames {
		if frame.page < 0 || frame.page >= len(l.pageBounds) {
			frame.frame.Hide()
			continue
		}
		bound := l.pageBounds[frame.page]
		frame.frame.Move(bound.position)
		frame.frame.Resize(bound.size)
		frame.frame.Show()
	}
}

// visibleRange 返回与当前视口相交的页面下标区间 [start, end)。页面边界按
// 纵向位置升序排列且互不重叠，因此可以二分定位，代价与页数无关。
func (l *continuousLayout) visibleRange() (int, int) {
	if len(l.pageBounds) == 0 {
		return 0, 0
	}
	top, bottom := l.visibleSpan()
	start := sort.Search(len(l.pageBounds), func(i int) bool {
		return l.pageBounds[i].position.Y+l.pageBounds[i].size.Height >= top
	})
	end := start + sort.Search(len(l.pageBounds)-start, func(i int) bool {
		return l.pageBounds[start+i].position.Y > bottom
	})
	return start, end
}

func (l *continuousLayout) visibleSpan() (float32, float32) {
	if l.pageScroll == nil {
		return 0, l.viewport.Height
	}
	top := l.pageScroll.Offset.Y
	return top, top + l.pageScroll.Size().Height
}

func (l *continuousLayout) pageAspect(index int) float32 {
	if index >= 0 && index < len(l.pages) && l.pages[index].aspect > 0 {
		return l.pages[index].aspect
	}
	return 1
}

// MinSize 返回完整内容尺寸，滚动容器据此计算滚动范围。
//
// 这里必须顺带重建排版结果：Fyne 的滚动容器按 max(内容尺寸, 视口) 设置内容
// 尺寸，缩小窗口时旧的内容尺寸仍然大于新视口，Content.Resize 会因尺寸未变
// 而跳过 Layout。若几何只能由 Layout 重建，缩小窗口后页面尺寸和视口都会
// 停留在旧值，新露出的页面也不会被渲染。
func (l *continuousLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	if len(l.frames) > 0 {
		l.ensureGeometry(l.resolveViewport(fyne.Size{}))
	}
	if l.minSize.Width > 0 && l.minSize.Height > 0 {
		return l.minSize
	}
	return fyne.NewSize(1, 1)
}

// refresh 请求滚动容器重新排版。几何失效由 layoutGeometry 的键负责，
// 这里不能单独清空 minSize，否则键未变时排版结果不会被重建。
func (l *continuousLayout) refresh() {
	if l.pageScroll != nil {
		l.pageScroll.Refresh()
	}
}

func (l *continuousLayout) setMode(mode pageViewMode) {
	l.mode = mode
}

func (l *continuousLayout) pageSize(aspect float32, viewport fyne.Size) fyne.Size {
	if aspect <= 0 {
		aspect = 1
	}
	availableWidth := fyne.Max(viewport.Width-2*l.margin, 1)
	availableHeight := fyne.Max(viewport.Height-2*l.margin, 1)
	switch l.mode {
	case viewFitWidth:
		return fyne.NewSize(availableWidth, availableWidth/aspect)
	case viewFitHeight:
		return fyne.NewSize(availableHeight*aspect, availableHeight)
	case viewDoublePage:
		pageWidth := fyne.Max((availableWidth-l.gap)/2, 1)
		return fyne.NewSize(pageWidth, pageWidth/aspect)
	default:
		scale := fyne.Min(availableWidth, availableHeight*aspect)
		return fyne.NewSize(scale, scale/aspect)
	}
}

// notifyViewportChange 在视口尺寸变化后通知外部补充渲染可见页面。
// 布局结束后调用，此时 pageBounds 已经填好，调用方可以直接按视口求可见页。
func (l *continuousLayout) notifyViewportChange() {
	if l.viewport == l.lastNotifiedViewport {
		return
	}
	l.lastNotifiedViewport = l.viewport
	if l.onViewportChange != nil {
		l.onViewportChange()
	}
}

// thumbnailCell 是缩略图列表的单元。点击跳转统一由 widget.List 的
// OnSelected 处理，这里不再单独实现 Tapped，否则一次点击会触发两次跳转。
type thumbnailCell struct {
	widget.BaseWidget
	content *fyne.Container
	image   *fyneCanvas.Image
	label   *widget.Label
}

func newThumbnailCell() *thumbnailCell {
	imageObject := fyneCanvas.NewImageFromImage(nil)
	imageObject.FillMode = fyneCanvas.ImageFillContain
	imageObject.ScaleMode = fyneCanvas.ImageScaleSmooth
	imageObject.SetMinSize(fyne.NewSize(130, 100))
	imageArea := container.NewStack(fyneCanvas.NewRectangle(color.White), imageObject)
	label := widget.NewLabel("")
	label.Alignment = fyne.TextAlignCenter
	cell := &thumbnailCell{
		content: container.NewVBox(imageArea, label),
		image:   imageObject,
		label:   label,
	}
	cell.ExtendBaseWidget(cell)
	return cell
}

func (c *thumbnailCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}

func (v *viewer) isDoublePage() bool {
	return v.pageLayout != nil && v.pageLayout.mode == viewDoublePage
}

func (v *viewer) hasPages() bool {
	return len(v.pages) > 0
}

func (v *viewer) pageAt(index int) (viewerPage, bool) {
	if index < 0 || index >= len(v.pages) {
		return viewerPage{}, false
	}
	return v.pages[index], true
}

func collectViewerPages(documents []*render.Document) []viewerPage {
	pages := make([]viewerPage, 0)
	for _, document := range documents {
		if document == nil || document.Document == nil {
			continue
		}
		for _, page := range document.Pages {
			if page == nil {
				continue
			}
			pages = append(pages, viewerPage{document: document, page: page})
		}
	}
	return pages
}

func (v *viewer) thumbnailRowCount() int {
	if !v.isDoublePage() {
		return v.totalPages
	}
	return (v.totalPages + 1) / 2
}

func (v *viewer) thumbnailRow(page int) int {
	if v.isDoublePage() {
		return page / 2
	}
	return page
}

func updateThumbnailCell(cell *thumbnailCell, page int, v *viewer) {
	cell.label.SetText(fmt.Sprintf("第 %d 页", page+1))
	if img, ok := v.thumbnails.Get(page); ok {
		cell.image.Image = img
	} else {
		cell.image.Image = nil
		v.requestThumbnailRender(page)
	}
	cell.image.Refresh()
}

func newViewer(window fyne.Window) *viewer {
	v := &viewer{window: window, thumbnailSelected: -1}
	v.pageImages = v.newPageImageCache()
	v.thumbnails = newThumbnailCache()
	v.thumbnailList = widget.NewList(
		func() int { return v.thumbnailRowCount() },
		func() fyne.CanvasObject {
			firstCell := newThumbnailCell()
			secondCell := newThumbnailCell()
			if !v.isDoublePage() {
				secondCell.Hide()
			}
			return container.NewHBox(firstCell, secondCell)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			row := item.(*fyne.Container)
			firstPage := id
			if v.isDoublePage() {
				firstPage *= 2
			}
			updateThumbnailCell(row.Objects[0].(*thumbnailCell), firstPage, v)
			secondPage := firstPage + 1
			secondCell := row.Objects[1].(*thumbnailCell)
			if v.isDoublePage() && secondPage < v.totalPages {
				secondCell.Show()
				updateThumbnailCell(secondCell, secondPage, v)
			} else {
				secondCell.Hide()
			}
		},
	)
	v.thumbnailList.HideSeparators = true
	v.thumbnailList.OnSelected = func(id widget.ListItemID) {
		if v.syncingThumbnail.Load() {
			return
		}
		v.thumbnailSelected = int(id)
		page := int(id)
		if v.isDoublePage() {
			page *= 2
		}
		v.goToPage(page, true)
	}

	v.pageLayout = &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	// 视口尺寸变化只能通过布局感知，滚动事件覆盖不到拖动窗口大小的场景。
	// 这里只补渲染，不改当前页等界面状态，避免在布局过程中改动控件。
	v.pageLayout.onViewportChange = func() {
		v.updatePageWindow(v.operation.Load())
	}
	v.pageContent = container.New(v.pageLayout)
	v.pageScroll = container.NewScroll(v.pageContent)
	v.pageScroll.Direction = container.ScrollVerticalOnly
	v.pageLayout.pageScroll = v.pageScroll
	v.pageScroll.OnScrolled = func(fyne.Position) {
		v.syncCurrentPage()
		v.updatePageWindow(v.operation.Load())
	}

	v.openButton = widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		v.chooseFile()
	})
	v.documentTitle = widget.NewLabelWithStyle("未加载文档", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	v.pageLabel = widget.NewLabel("/ 未加载文档")
	v.pageEntry = widget.NewEntry()
	v.pageEntry.SetPlaceHolder("页码")
	v.pageEntry.OnSubmitted = func(string) {
		v.jumpToPage()
	}
	v.thumbnailToggle = widget.NewButtonWithIcon("", theme.ListIcon(), func() {
		v.setThumbnailVisible(!v.thumbnailPanel.Visible())
	})
	v.thumbnailToggle.Importance = widget.LowImportance
	v.menuButton = widget.NewButtonWithIcon("", theme.MenuIcon(), func() {
		v.showMenu()
	})

	leftToolbar := container.NewHBox(
		v.openButton,
		widget.NewSeparator(),
		v.thumbnailToggle,
	)
	pageToolbar := container.NewHBox(v.pageEntry, v.pageLabel)
	pageToolbar.Hide()
	v.pageToolbar = pageToolbar
	rightToolbar := container.NewHBox(pageToolbar, v.menuButton)
	toolbar := container.NewBorder(nil, nil, leftToolbar, rightToolbar, container.NewCenter(v.documentTitle))
	thumbnailPanel := container.NewBorder(widget.NewLabel("页面"), nil, nil, nil, v.thumbnailList)
	thumbnailPanel.Hide()
	documentArea := container.NewHSplit(thumbnailPanel, v.pageScroll)
	documentArea.SetOffset(0.2)
	v.thumbnailPanel = thumbnailPanel
	v.documentArea = documentArea
	v.content = container.NewBorder(toolbar, nil, nil, nil, documentArea)
	v.updateControls()
	return v
}

// newPageImageCache 创建阅读区页面图像缓存。页面图像在 96 DPI 下体积可观，
// 必须按字节预算限流，否则滚动过的大文档会累积数 GB 内存。
func (v *viewer) newPageImageCache() *utils.LRU[int, *image.RGBA] {
	return utils.NewWeightedLRU[int, *image.RGBA](pageImageEntries, pageImageBudget,
		func(page int, _ *image.RGBA) {
			// 只有仍在显示窗口内的页面会占用帧，而 canEvict 已经把它们排除，
			// 因此这里通常找不到帧；保留判断以防窗口状态在回调前变化。
			frame := v.frameForPage(page)
			if frame == nil {
				return
			}
			frame.image.Image = nil
			frame.image.Refresh()
		},
		// 被显示帧引用的页面不淘汰，否则刚滚动到就会显示空白。
		func(page int, _ *image.RGBA) bool {
			return !v.protectPageImage(page)
		},
	)
}

// newThumbnailCache 创建缩略图缓存。淘汰回调不触碰列表：列表复用可见单元，
// 缓存条目移除后单元格仍可显示已上传的纹理，等下次滚动回来再重新渲染，
// 因此不会因为淘汰回调触发新的渲染而形成抖动。
func newThumbnailCache() *utils.LRU[int, *image.RGBA] {
	return utils.NewWeightedLRU[int, *image.RGBA](thumbnailEntries, thumbnailBudget, nil)
}

func (v *viewer) pageSlotAt(page int) *pageMeta {
	if page < 0 || page >= len(v.pageSlots) {
		return nil
	}
	return v.pageSlots[page]
}

// frameForPage 返回当前显示该页的帧；页面不在显示窗口内时返回 nil。
func (v *viewer) frameForPage(page int) *pageFrame {
	for _, frame := range v.pageLayout.frames {
		if frame.page == page {
			return frame
		}
	}
	return nil
}

// protectPageImage 判断页面图像是否仍被显示帧引用。被帧引用的位图不能淘汰：
// 画面会留白，而且位图仍被帧持有，淘汰也释放不了内存。
func (v *viewer) protectPageImage(page int) bool {
	return v.frameForPage(page) != nil
}

func (v *viewer) showMenu() {
	if v.closed.Load() {
		return
	}
	exportItem := fyne.NewMenuItemWithIcon("导出", theme.DocumentSaveIcon(), v.showExportDialog)
	exportItem.Disabled = !v.hasPages() || v.loading || v.exporting
	viewItems := []*fyne.MenuItem{
		fyne.NewMenuItem(viewFitPageLabel, func() { v.setViewMode(viewFitPageLabel) }),
		fyne.NewMenuItem(viewFitWidthLabel, func() { v.setViewMode(viewFitWidthLabel) }),
		fyne.NewMenuItem(viewFitHeightLabel, func() { v.setViewMode(viewFitHeightLabel) }),
		fyne.NewMenuItem(viewDoublePageLabel, func() { v.setViewMode(viewDoublePageLabel) }),
	}
	viewModes := []pageViewMode{viewFitPage, viewFitWidth, viewFitHeight, viewDoublePage}
	for i, item := range viewItems {
		item.Checked = v.pageLayout.mode == viewModes[i]
		item.Disabled = v.loading || v.exporting
	}
	viewItem := fyne.NewMenuItem("视图", nil)
	viewItem.ChildMenu = fyne.NewMenu("视图", viewItems...)
	closeLabel := "退出程序"
	if v.hasPages() {
		closeLabel = "关闭文档"
	}
	closeItem := fyne.NewMenuItemWithIcon(closeLabel, theme.CancelIcon(), v.closeDocumentOrExit)
	closeItem.Disabled = v.loading || v.exporting
	menu := fyne.NewMenu("菜单",
		exportItem,
		viewItem,
		closeItem,
		fyne.NewMenuItemWithIcon("关于", theme.InfoIcon(), v.showAppInfo),
	)
	canvas := v.window.Canvas()
	widget.ShowPopUpMenuAtRelativePosition(menu, canvas, fyne.NewPos(0, v.menuButton.Size().Height), v.menuButton)
}

func (v *viewer) showExportDialog() {
	if v.closed.Load() || !v.hasPages() || v.loading || v.exporting {
		return
	}
	// 大开本文档的页面上限可能低于默认 DPI，先把默认值夹到上限内，
	// 否则用户不改任何设置直接确定就会看到导出失败。
	dpiLimit := maxExportDPI(v.maxPageArea)
	defaultDPI := exportDPI
	if defaultDPI > dpiLimit {
		defaultDPI = dpiLimit
	}
	dpiEntry := widget.NewEntry()
	dpiEntry.SetText(strconv.Itoa(defaultDPI))
	dpiEntry.SetPlaceHolder(fmt.Sprintf("%d-%d（受页面尺寸限制）", exportMinDPI, dpiLimit))
	formatSelect := widget.NewSelect([]string{
		exportFormatPDF,
		exportFormatTXT,
		exportFormatJPG,
		exportFormatPNG,
		exportFormatSVG,
		exportFormatEPS,
		exportFormatTeX,
	}, nil)
	formatSelect.SetSelected(exportFormatPDF)
	dpiItem := widget.NewFormItem("DPI", dpiEntry)
	backgroundSelect := widget.NewSelect([]string{exportBackgroundTransparent, exportBackgroundWhite}, nil)
	backgroundSelect.SetSelected(exportBackgroundTransparent)
	content := widget.NewForm(
		widget.NewFormItem("格式", formatSelect),
		widget.NewFormItem("背景颜色", backgroundSelect),
	)
	dpiVisible := false
	formatSelect.OnChanged = func(selected string) {
		visible := selected == exportFormatJPG || selected == exportFormatPNG
		if visible == dpiVisible {
			return
		}
		dpiVisible = visible
		if visible {
			content.Items = append([]*widget.FormItem{dpiItem}, content.Items...)
		} else {
			content.RemoveItem(dpiItem)
		}
		content.Refresh()
	}
	exportDialog := dialog.NewCustomConfirm("导出文档", "导出", "取消", content, func(confirmed bool) {
		if !confirmed {
			return
		}
		dpi := defaultDPI
		if formatSelect.Selected == exportFormatJPG || formatSelect.Selected == exportFormatPNG {
			var err error
			dpi, err = strconv.Atoi(strings.TrimSpace(dpiEntry.Text))
			if err != nil || dpi < exportMinDPI || dpi > exportMaxDPI {
				dialog.ShowInformation("导出失败", fmt.Sprintf("DPI 必须是 %d-%d 之间的整数。", exportMinDPI, exportMaxDPI), v.window)
				return
			}
			// 1200 DPI 对大页面仍会产生数百 MB 的单页栅格，这里按页面实际
			// 尺寸再收紧一次，避免导出时被系统 OOM 杀掉。
			if dpi > dpiLimit {
				dialog.ShowInformation("导出失败", fmt.Sprintf("当前页面尺寸下 DPI 不能超过 %d，否则单页栅格会超过 %d MB。", dpiLimit, exportPageBudget>>20), v.window)
				return
			}
		}
		v.export(exportFormatCode(formatSelect.Selected), dpi, exportBackgroundColor(backgroundSelect.Selected))
	}, v.window)
	exportDialog.Show()
}

func exportBackgroundColor(label string) color.Color {
	if label == exportBackgroundWhite {
		return color.White
	}
	return color.Transparent
}

// sameBackground 按渲染结果比较背景色。直接比较 color.Color 接口会在动态类型
// 不可比较时 panic，这里统一走 RGBA 分量。
func sameBackground(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func exportFormatCode(label string) string {
	switch label {
	case exportFormatTXT:
		return "txt"
	case exportFormatJPG:
		return "jpg"
	case exportFormatPNG:
		return "png"
	case exportFormatSVG:
		return "svg"
	case exportFormatEPS:
		return "eps"
	case exportFormatTeX:
		return "tex"
	default:
		return "pdf"
	}
}

// exportExtension 返回导出文件扩展名。PDF 和 TXT 是整文档单文件；其余格式
// 只有单页时直接写文件，多页时打包成 ZIP。
func exportExtension(format string, totalPages int) string {
	format = strings.ToLower(format)
	if format == "pdf" || format == "txt" || totalPages <= 1 {
		return format
	}
	return "zip"
}

func (v *viewer) export(format string, dpi int, background color.Color) {
	if v.closed.Load() || v.loading || !v.hasPages() || v.exporting {
		return
	}
	extension := exportExtension(format, v.totalPages)
	fileName := strings.TrimSuffix(v.fileName, filepath.Ext(v.fileName)) + "." + extension
	v.exporting = true
	v.updateControls()
	go func() {
		selection, err := chooseSaveFile("导出 OFD", fileName, extension, v.window)
		fyne.Do(func() {
			if v.closed.Load() {
				if selection.output != nil {
					_ = selection.output.Close()
				}
				return
			}
			if err != nil {
				v.finishExport()
				dialog.ShowInformation("导出失败", err.Error(), v.window)
				return
			}
			if selection.path == "" && selection.output == nil {
				v.finishExport()
				return
			}
			if selection.output != nil {
				v.exportToWriter(selection.output, format, dpi, background)
			} else {
				v.exportToPath(selection.path, format, dpi, background)
			}
		})
	}()
}

func (v *viewer) exportToPath(path, format string, dpi int, background color.Color) {
	if path == "" {
		v.finishExport()
		return
	}
	v.exportToOutput(func() (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}, nil, format, dpi, background)
}

func (v *viewer) exportToWriter(writer io.WriteCloser, format string, dpi int, background color.Color) {
	if writer == nil {
		v.finishExport()
		return
	}
	v.exportToOutput(func() (io.WriteCloser, error) {
		return writer, nil
	}, writer, format, dpi, background)
}

// finishExport 结束导出状态。v.exporting 只在 export 里置位，因此每一条离开
// export 的路径都必须复位一次；漏掉会让界面永久保持禁用。
func (v *viewer) finishExport() {
	v.exporting = false
	v.updateControls()
}

func (v *viewer) exportToOutput(create func() (io.WriteCloser, error), pending io.WriteCloser, format string, dpi int, background color.Color) {
	if v.closed.Load() || v.loading || !v.hasPages() {
		if pending != nil {
			_ = pending.Close()
		}
		v.finishExport()
		return
	}
	v.showExportLoading()
	exportOperation := v.operation.Load()
	session := v.session
	documents := append([]*render.Document(nil), v.documents...)
	go func() {
		var err error
		if exportOperation != v.operation.Load() {
			err = errDocumentChanged
			if pending != nil {
				_ = pending.Close()
			}
		} else {
			err = writeExport(session, documents, create, pending, format, dpi, background)
		}
		fyne.Do(func() {
			if v.closed.Load() {
				return
			}
			v.hideExportLoading()
			v.finishExport()
			if err != nil {
				dialog.ShowInformation("导出失败", err.Error(), v.window)
				return
			}
			dialog.ShowInformation("导出完成", "文件已成功导出。", v.window)
		})
	}()
}

// writeExport 执行一次导出，阻塞到完成。会话引用在整个导出期间保持，底层
// 文档不会被关闭；引用失败说明文档已被替换或关闭。
//
// 刻意做成同步函数：导出涉及会话引用、外部写入器的所有权和多种错误收尾，
// 独立出来才能直接验证，不必经过 exportToOutput 的后台协程。
func writeExport(session *documentSession, documents []*render.Document, create func() (io.WriteCloser, error), pending io.WriteCloser, format string, dpi int, background color.Color) error {
	if !session.acquire() {
		if pending != nil {
			_ = pending.Close()
		}
		return errDocumentChanged
	}
	defer session.release()
	writer, err := create()
	if err != nil {
		return err
	}
	err = exportDocumentsToWriter(documents, writer, format, dpi, background)
	// 写入器的所有权在导出期间属于这里，成功失败都要关闭。
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (v *viewer) showExportLoading() {
	if v.exportLoading != nil {
		return
	}
	progress := widget.NewProgressBarInfinite()
	title := widget.NewLabelWithStyle("正在导出", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	message := widget.NewLabel("正在生成文件，请稍候...")
	content := container.NewPadded(container.NewVBox(title, message, progress))
	v.exportLoading = widget.NewModalPopUp(content, v.window.Canvas())
	v.exportLoading.Show()
}

func (v *viewer) hideExportLoading() {
	if v.exportLoading == nil {
		return
	}
	v.exportLoading.Hide()
	v.exportLoading = nil
}

func exportDocumentsToWriter(documents []*render.Document, output io.Writer, format string, dpi int, background color.Color) error {
	if len(documents) == 0 {
		return fmt.Errorf("文档没有页面")
	}
	if output == nil {
		return fmt.Errorf("未设置导出输出")
	}
	exportDocs := make([]*render.Document, 0, len(documents))
	pageCount := 0
	for _, doc := range documents {
		if doc == nil || doc.Document == nil {
			continue
		}
		exportDoc := exportDocument(doc, background)
		exportDocs = append(exportDocs, exportDoc)
		for _, page := range exportDoc.Pages {
			if page != nil {
				pageCount++
			}
		}
	}
	if pageCount == 0 {
		return fmt.Errorf("文档没有页面")
	}
	if strings.EqualFold(format, "txt") {
		parsedDocs := make([]*parser.Document, 0, len(exportDocs))
		for _, doc := range exportDocs {
			parsedDocs = append(parsedDocs, doc.Document)
		}
		return canvasConverter.TextDocuments(parsedDocs, output)
	}
	if strings.EqualFold(format, "pdf") {
		return canvasConverter.PDFDocuments(exportDocs, output)
	}
	option := exportImageOption(format)
	imageOptions := []canvasConverter.Option{
		canvasConverter.DPI(float64(dpi)),
		option,
	}
	if pageCount == 1 {
		return canvasConverter.ImageDocuments(exportDocs,
			append(imageOptions, canvasConverter.Writer(func(int) (io.WriteCloser, error) {
				return &noCloseWriter{Writer: output}, nil
			}))...,
		)
	}

	archive := zip.NewWriter(output)
	extension := strings.ToLower(format)
	err := canvasConverter.ImageDocuments(exportDocs,
		append(imageOptions,
			canvasConverter.Writer(func(page int) (io.WriteCloser, error) {
				entry, err := archive.Create(fmt.Sprintf("page-%04d.%s", page, extension))
				if err != nil {
					return nil, err
				}
				return &noCloseWriter{Writer: entry}, nil
			}))...,
	)
	if err != nil {
		archive.Close()
		return err
	}
	return archive.Close()
}

// exportDocument 取得用于导出的渲染文档。背景色一致时直接复用阅读区的那个，
// 避免为导出再复制一套字体和图片缓存；导出对话框默认就是透明背景，命中的
// 是常见路径。
func exportDocument(doc *render.Document, background color.Color) *render.Document {
	if doc == nil || doc.Document == nil {
		return nil
	}
	if sameBackground(doc.Background(), background) {
		return doc
	}
	return render.NewDocument(background, doc.Document)
}

func exportImageOption(format string) canvasConverter.Option {
	switch strings.ToLower(format) {
	case "jpg":
		return canvasConverter.JPG()
	case "svg":
		return canvasConverter.SVG()
	case "eps":
		return canvasConverter.EPS()
	case "tex":
		return canvasConverter.TeX()
	default:
		return canvasConverter.PNG()
	}
}

// noCloseWriter 隐藏底层写入器的 Close。ZIP 条目和已打开的输出流都由外层
// 负责关闭，交给转换器再次关闭会破坏输出。
type noCloseWriter struct {
	io.Writer
}

func (w *noCloseWriter) Close() error {
	return nil
}

func (v *viewer) showAppInfo() {
	link, err := url.Parse(projectURL)
	if err != nil {
		return
	}
	icon := fyneCanvas.NewImageFromResource(viewerIcon)
	icon.FillMode = fyneCanvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(96, 96))
	content := container.NewVBox(
		container.NewCenter(icon),
		widget.NewLabelWithStyle("OFD Viewer", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("版本: "+applicationVersion),
		widget.NewLabel("OFD 文档查看器"),
		widget.NewLabel("应用 ID: "+applicationID),
		widget.NewLabel("项目地址:"),
		widget.NewHyperlink(projectURL, link),
	)
	dialog.NewCustom("关于", "关闭", content, v.window).Show()
}

// pageSlotStates 计算每个页面的槽位参数。页面区域无法解析时仍然返回条目，
// 保证槽位下标与全局页码一一对应；否则布局和跳转会访问空槽位而崩溃。
// normalizePageBox 补齐页面物理尺寸。PhysicalBoxMetadata 不加载页面内容，
// 页面没有有效尺寸时返回零值，此时回退到文档的公共页面区域，再回退到 A4。
//
// 与 parser 的差异：原实现会先对页面自己的 Area 调用 EnsurePhysicalBox，
// 因此"Area 存在但宽高非法"的页面会直接得到 A4；这里会先尝试文档公共区域。
// 只有畸形输入才会走到这个分支，两种回退都是任意的。
func normalizePageBox(box models.StBox, doc *render.Document) models.StBox {
	if box.Width > 0 && box.Height > 0 {
		return box
	}
	area := models.CtPageArea{}
	if doc != nil {
		area = doc.CommonData.PageArea
	}
	area.EnsurePhysicalBox()
	return area.PhysicalBox
}

func pageSlotStates(pages []viewerPage) []pageSlotState {
	states := make([]pageSlotState, len(pages))
	for i, pageRef := range pages {
		states[i] = pageSlotState{aspect: 1}
		// 这里只需要宽高比和面积。用 PhysicalBox 会为每一页加载完整内容与
		// 资源、并把页面塞进页面缓存，万页文档会在事件线程上卡住数秒。
		// PhysicalBoxMetadata 只读页面 XML 的 Area，不占租约也不进缓存。
		box, err := pageRef.page.PhysicalBoxMetadata()
		if err != nil {
			slog.Error("读取页面失败", "page", i, "error", err)
			continue
		}
		box = normalizePageBox(box, pageRef.document)
		if box.Height > 0 {
			states[i].aspect = float32(box.Width / box.Height)
		}
		states[i].area = box.Width * box.Height
		states[i].renderable = true
	}
	return states
}

// createPageSlots 按页面建立槽位。states 由 buildDocumentModel 在后台算好；
// 长度不匹配时在此补算，供测试和未走 runLoad 的路径使用。
func (v *viewer) createPageSlots(pages []viewerPage, states []pageSlotState) {
	if len(states) != len(pages) {
		states = pageSlotStates(pages)
	}
	metas := make([]*pageMeta, len(states))
	maxArea := 0.0
	for i, state := range states {
		if state.area > maxArea {
			maxArea = state.area
		}
		metas[i] = &pageMeta{
			aspect:     state.aspect,
			area:       state.area,
			renderable: state.renderable,
		}
	}
	v.maxPageArea = maxArea
	v.pageSlots = metas
	// 显示帧数量只与视口有关，换文档时按新页数重建排版结果。
	v.pageLayout.setPages(metas)
	v.pageContent.Objects = v.pageLayout.frameObjects()
	v.pageContent.Refresh()
	v.pageScroll.Refresh()
}

func (v *viewer) chooseFile() {
	if v.closed.Load() || v.loading || v.exporting {
		return
	}
	v.loading = true
	v.updateControls()
	go func() {
		selection, err := chooseOpenFile("选择 OFD 文件", v.window)
		fyne.Do(func() {
			if v.closed.Load() {
				if selection.input != nil {
					if closer, ok := selection.input.(io.Closer); ok {
						_ = closer.Close()
					}
				}
				return
			}
			if err != nil {
				v.loading = false
				v.updateControls()
				dialog.ShowInformation("打开失败", err.Error(), v.window)
				return
			}
			if selection.path == "" && selection.input == nil {
				v.loading = false
				v.updateControls()
				return
			}
			v.load(selection.path, selection.name, selection.input)
		})
	}()
}

func (v *viewer) load(filePath, fileName string, input any) {
	if v.closed.Load() {
		_ = closeInput(input)
		return
	}
	if input == nil && filePath == "" {
		return
	}
	v.hidePageLoading(0)
	operation := v.operation.Add(1)
	v.thumbnailGeneration.Add(1)
	v.loading = true
	v.updateControls()
	go v.runLoad(operation, filePath, fileName, input)
}

// runLoad 解析输入并在 Fyne 事件线程上发布文档。解析在调用方线程完成，
// 界面状态只在 fyne.Do 回调里改动。
//
// 独立于 load 是为了让这条关键路径可以被同步验证：测试可以直接调用它，
// 不必与后台协程和测试驱动内联执行的 fyne.Do 纠缠。
func (v *viewer) runLoad(operation uint64, filePath, fileName string, input any) {
	slog.Debug("正在打开文件", "path", filePath)
	ofd, err := openOFD(input)
	if closeErr := closeInput(input); closeErr != nil && err == nil {
		err = closeErr
	}

	fyne.Do(func() {
		published := false
		defer func() {
			if recovered := recover(); recovered != nil {
				// 已经发布出去的文档归会话所有，不能在这里关闭。
				if ofd != nil && !published {
					_ = ofd.Close()
				}
				if operation == v.operation.Load() && !v.closed.Load() {
					v.finishLoad()
					slog.Error("打开 OFD 发生 panic", "error", recovered, "stack", string(debug.Stack()))
					dialog.ShowInformation("打开失败", fmt.Sprintf("处理 OFD 文件失败: %v", recovered), v.window)
				}
			}
		}()
		if operation != v.operation.Load() || v.closed.Load() {
			if ofd != nil {
				_ = ofd.Close()
			}
			return
		}
		// parser.NewOFD 失败时同样返回非 nil 的 OFD（只是没有资源），所以
		// 必须先看 err：只判断 ofd != nil 会把真实原因换成“没有文档”。
		var model *documentModel
		if err == nil {
			// 构造模型要按页读 XML，必须留在后台线程，不能占用事件线程。
			model, err = buildDocumentModel(ofd, fileName, color.Transparent)
		}
		if err == nil {
			err = v.publishDocument(model, filePath, fileName)
		}
		if err != nil {
			if ofd != nil {
				_ = ofd.Close()
			}
			v.finishLoad()
			slog.Error("打开 OFD 失败", "error", err)
			dialog.ShowInformation("打开失败", err.Error(), v.window)
			return
		}
		published = true
		v.finishLoad()
	})
}

// finishLoad 结束加载状态并刷新控件。v.loading 只在 load 里置位，因此每一条
// 离开加载流程的路径都必须复位一次；漏掉会让界面永久保持禁用。
func (v *viewer) finishLoad() {
	v.loading = false
	v.updateControls()
}

// documentModel 是一次解析的完整结果：渲染文档、页面列表，以及每页的排版
// 参数。构造它需要按页读取 XML，因此在后台线程完成；发布它只改界面状态。
type documentModel struct {
	ofd       *parser.OFD
	documents []*render.Document
	pages     []viewerPage
	states    []pageSlotState
	title     string
}

// buildDocumentModel 构造可发布的文档模型。读取每页的物理尺寸是按页的 XML
// I/O：万页文档在事件线程上要 100ms 以上，必须留在后台。
func buildDocumentModel(ofd *parser.OFD, fileName string, background color.Color) (*documentModel, error) {
	if ofd == nil || len(ofd.Documents) == 0 {
		return nil, fmt.Errorf("没有文档")
	}
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(background, document))
	}
	pages := collectViewerPages(documents)
	if len(pages) == 0 {
		return nil, fmt.Errorf("文档没有页面")
	}
	states := pageSlotStates(pages)
	if len(states) != len(pages) {
		return nil, fmt.Errorf("文档没有页面")
	}
	return &documentModel{
		ofd:       ofd,
		documents: documents,
		pages:     pages,
		states:    states,
		title:     documentTitle(ofd, fileName),
	}, nil
}

// publishDocument 把已解析的文档发布为当前阅读对象，替换此前的文档并重建
// 显示状态。失败时返回错误并保持原状态不变。
//
// 只在 Fyne 事件线程上改动界面状态，模型本身由 buildDocumentModel 在后台
// 准备好。独立成同步函数是为了能直接验证，不必经过 load 的后台协程。
func (v *viewer) publishDocument(model *documentModel, filePath, fileName string) error {
	if model == nil || model.ofd == nil || len(model.pages) == 0 || len(model.states) != len(model.pages) {
		return fmt.Errorf("没有文档")
	}

	// 校验通过后才动旧状态：发布失败时用户仍能看到原文档。
	v.closeDocument()
	v.ofd = model.ofd
	v.session = newDocumentSession(model.ofd)
	v.documents = model.documents
	v.pages = model.pages
	v.documentTitle.SetText(model.title)
	v.filePath = filePath
	v.fileName = fileName
	v.currentPage = 0
	v.totalPages = len(model.pages)
	v.pageEntry.SetText("1")
	v.thumbnails = newThumbnailCache()
	v.pageImages = v.newPageImageCache()
	v.thumbnailRendering = make([]atomic.Bool, v.totalPages)
	v.createPageSlots(model.pages, model.states)
	v.pageScroll.ScrollToTop()
	v.thumbnailList.Refresh()
	v.thumbnailSelected = 0
	v.thumbnailList.Select(0)
	v.updateTitle()
	v.updateControls()
	v.updatePageWindow(v.operation.Load())
	return nil
}

// expandPageWindow 把可视区间向两侧各扩展 margin 页，并夹到 [0, totalPages)。
// 纯函数，便于回归测试。
func expandPageWindow(start, end, totalPages, margin int) (int, int) {
	if margin < 0 {
		margin = 0
	}
	if start < 0 {
		start = 0
	}
	if end > totalPages {
		end = totalPages
	}
	if end < start {
		end = start
	}
	if start-margin > 0 {
		start -= margin
	} else {
		start = 0
	}
	if end+margin < totalPages {
		end += margin
	} else {
		end = totalPages
	}
	return start, end
}

// updatePageWindow 把当前视口内及相邻页绑定到显示帧，并按需渲染这些页面。
// 可视区间由 pageBounds 二分得到，因此与文档页数无关。
func (v *viewer) updatePageWindow(operation uint64) {
	if operation == 0 || !v.hasPages() || len(v.pageLayout.pageBounds) != len(v.pageSlots) {
		return
	}
	visibleStart, visibleEnd := v.pageLayout.visibleRange()
	start, end := expandPageWindow(visibleStart, visibleEnd, v.totalPages, pagePrefetch)
	v.bindPageWindow(start, end)
	for page := start; page < end; page++ {
		v.requestPageRender(operation, page)
	}
	// 刚滚出显示窗口的页面此前受保护而未淘汰，这里补一次清理。
	v.pageImages.Trim()
}

// bindPageWindow 将 [start, end) 区间的页面依次绑定到显示帧，多余的帧回收。
// 换页时把帧里的图像换成新页面的缓存位图，避免显示上一页的残留。
func (v *viewer) bindPageWindow(start, end int) {
	if start < 0 {
		start = 0
	}
	if end > v.totalPages {
		end = v.totalPages
	}
	if end < start {
		end = start
	}
	wanted := end - start
	if wanted > len(v.pageLayout.frames) {
		// 视口能容纳的页面比预估的多，按需扩池。常见页面尺寸不会走到这里。
		v.pageLayout.growFrames(wanted * 2)
		v.pageContent.Objects = v.pageLayout.frameObjects()
	}
	changed := false
	for i, frame := range v.pageLayout.frames {
		page := -1
		if i < wanted {
			page = start + i
		}
		if frame.page == page {
			continue
		}
		frame.page = page
		changed = true
		frame.image.Image = nil
		if page >= 0 {
			if raster, ok := v.pageImages.Get(page); ok {
				frame.image.Image = raster
			}
		}
		frame.image.Refresh()
	}
	if changed {
		// 重新排布显示帧。Container.Refresh 会同步再跑一次 Layout，
		// notifyViewportChange 因为视口未变不会再回调，最多嵌套一层。
		v.pageContent.Refresh()
	}
}

// requestPageRender 发起按需渲染，返回是否真的启动了渲染任务。调用方据此
// 决定要不要显示加载提示：没有任务就没有会关闭提示的回调。
func (v *viewer) requestPageRender(operation uint64, pageIndex int) bool {
	if operation == 0 || !v.hasPages() || pageIndex < 0 || pageIndex >= len(v.pageSlots) {
		return false
	}
	slot := v.pageSlots[pageIndex]
	if !slot.renderable {
		return false
	}
	// 已缓存的页面只需刷新最近使用顺序，避免被 LRU 优先淘汰。
	if _, cached := v.pageImages.Get(pageIndex); cached {
		return false
	}
	if !slot.rendering.CompareAndSwap(false, true) {
		return false
	}
	pageRef, ok := v.pageAt(pageIndex)
	if !ok {
		slot.rendering.Store(false)
		return false
	}
	go v.renderPage(operation, pageRef.document, pageIndex, pageRef.page, slot)
	return true
}

func (v *viewer) renderPage(operation uint64, doc *render.Document, pageIndex int, page *parser.Page, slot *pageMeta) {
	img, err := v.renderPageImage(doc, page, geom.DPI(viewerDPI), func() bool {
		return operation == v.operation.Load()
	})
	if err != nil || img == nil {
		slot.rendering.Store(false)
		if err != nil {
			slog.Error("渲染页面失败", "page", pageIndex, "error", err)
		}
		fyne.Do(func() {
			if operation == v.operation.Load() && pageIndex == v.currentPage {
				v.hidePageLoading(operation)
			}
		})
		return
	}
	fyne.Do(func() {
		slot.rendering.Store(false)
		if operation != v.operation.Load() {
			return
		}
		// 先入缓存再更新帧：淘汰回调可能清掉其它页面的帧。
		v.pageImages.AddWeighted(pageIndex, img, rasterWeight(img))
		if frame := v.frameForPage(pageIndex); frame != nil {
			frame.image.Image = img
			frame.image.Refresh()
		}
		if pageIndex == v.currentPage {
			v.hidePageLoading(operation)
		}
	})
}

func (v *viewer) requestThumbnailRender(pageIndex int) {
	generation := v.thumbnailGeneration.Load()
	if generation == 0 || !v.hasPages() || pageIndex < 0 || pageIndex >= len(v.pages) || pageIndex >= len(v.thumbnailRendering) {
		return
	}
	// 页面区域读不出来的槽位不能渲染。列表每次滚动到该行都会再请求一次，
	// 不检查就会反复触发注定失败的渲染并刷屏错误日志。
	slot := v.pageSlotAt(pageIndex)
	if slot == nil || !slot.renderable {
		return
	}
	if _, cached := v.thumbnails.Get(pageIndex); cached {
		return
	}
	if !v.thumbnailRendering[pageIndex].CompareAndSwap(false, true) {
		return
	}
	rendering := &v.thumbnailRendering[pageIndex]
	pageRef, ok := v.pageAt(pageIndex)
	if !ok {
		rendering.Store(false)
		return
	}
	go func() {
		img, err := v.renderPageImage(pageRef.document, pageRef.page, geom.DPI(thumbnailDPI), func() bool {
			return generation == v.thumbnailGeneration.Load()
		})
		rendering.Store(false)
		if err != nil || img == nil {
			if err != nil {
				slog.Error("渲染缩略图失败", "page", pageIndex, "error", err)
			}
			return
		}
		fyne.Do(func() {
			if generation != v.thumbnailGeneration.Load() {
				return
			}
			v.thumbnails.AddWeighted(pageIndex, img, rasterWeight(img))
			v.thumbnailList.RefreshItem(v.thumbnailRow(pageIndex))
		})
	}()
}

func openOFD(input any) (ofd *parser.OFD, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if ofd != nil {
				_ = ofd.Close()
				ofd = nil
			}
			slog.Error("解析 OFD 发生 panic", "error", recovered, "stack", string(debug.Stack()))
			err = fmt.Errorf("打开 OFD 失败: %v", recovered)
		}
	}()
	return parser.NewOFD(input)
}

func closeInput(input any) (err error) {
	closer, ok := input.(io.Closer)
	if !ok || closer == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("关闭文件失败: %v", recovered)
		}
	}()
	return closer.Close()
}

func (v *viewer) renderPageImage(doc *render.Document, page *parser.Page, resolution geom.Resolution, valid func() bool) (raster *image.RGBA, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			raster = nil
			slog.Error("渲染页面发生 panic", "error", recovered, "stack", string(debug.Stack()))
			err = fmt.Errorf("渲染页面失败: %v", recovered)
		}
	}()
	if doc == nil || doc.Document == nil || page == nil {
		return nil, fmt.Errorf("页面数据为空")
	}
	if !valid() {
		return nil, nil
	}
	// 直接按指定分辨率使用内置 canvas 后端栅格化页面。此前通过 Fyne 画布
	// 渲染再取图，最终同样走 canvas 光栅器，因此输出一致；改为 RasterizePage
	// 后核心渲染不再需要 *canvas.Context 适配。
	//
	// 这里不加互斥锁：RasterizePage 全程持有解析器页面租约，文档关闭会等到
	// 租约释放，因此并发渲染同一文档是安全的，也让多页并行渲染真正生效。
	raster, err = doc.RasterizePage(page, render.BackendCanvas, resolution)
	if err != nil {
		return nil, err
	}
	return raster, nil
}

func (v *viewer) changePage(delta int) {
	if v.loading || !v.hasPages() || v.totalPages == 0 {
		return
	}
	next := v.currentPage + delta
	if next < 0 || next >= v.totalPages {
		return
	}
	v.goToPage(next, false)
}

func (v *viewer) handleKey(event *fyne.KeyEvent) {
	switch event.Name {
	case fyne.KeyLeft, fyne.KeyUp:
		v.changePage(-1)
	case fyne.KeyRight, fyne.KeyDown:
		v.changePage(1)
	case fyne.KeyPageUp:
		v.scrollByViewport(-1)
	case fyne.KeyPageDown:
		v.scrollByViewport(1)
	case fyne.KeyHome:
		v.goToPage(0, false)
	case fyne.KeyEnd:
		v.goToPage(v.totalPages-1, false)
	case mobile.KeyBack:
		if runtime.GOOS == "android" {
			v.handleAndroidBack()
		} else {
			v.closeDocumentOrExit()
		}
	case fyne.KeyEscape:
		// 与菜单项、Android 返回键一致地逐层退出：先关文档，没有文档才退程序。
		v.closeDocumentOrExit()
	default:
		switch strings.ToLower(string(event.Name)) {
		case "o":
			v.chooseFile()
		case "a", "w":
			v.changePage(-1)
		case "d", "s":
			v.changePage(1)
		case "q":
			v.closeDocumentOrExit()
		}
	}
}

const androidBackPressWindow = 2 * time.Second

func (v *viewer) handleAndroidBack() {
	if v.closed.Load() || v.loading || v.exporting {
		return
	}
	now := time.Now()
	if now.Before(v.backDeadline) {
		v.backDeadline = time.Time{}
		dialog.ShowConfirm("退出程序", "确定要退出 OFD Viewer 吗？", func(confirmed bool) {
			if confirmed {
				v.exitApplication()
				return
			}
			v.documentTitle.SetText("未加载文档")
		}, v.window)
		return
	}

	v.backDeadline = now.Add(androidBackPressWindow)
	if v.ofd != nil || v.hasPages() {
		v.operation.Add(1)
		v.thumbnailGeneration.Add(1)
		v.closeDocument()
	}
	v.documentTitle.SetText("再次按返回键退出")
	deadline := v.backDeadline
	time.AfterFunc(androidBackPressWindow, func() {
		fyne.Do(func() {
			if v.closed.Load() || v.backDeadline != deadline || v.hasPages() {
				return
			}
			v.backDeadline = time.Time{}
			v.documentTitle.SetText("未加载文档")
		})
	})
}

func (v *viewer) goToPage(page int, showLoading bool) {
	if v.loading || !v.hasPages() || page < 0 || page >= v.totalPages {
		return
	}
	if page == v.currentPage && len(v.pageLayout.pageBounds) <= page {
		return
	}
	v.currentPage = page
	v.pageEntry.SetText(strconv.Itoa(page + 1))
	v.scrollToPage(page)
	// ScrollToOffset 不会触发 OnScrolled，虚拟化的显示窗口必须在这里重建，
	// 否则目标页没有对应的显示帧，渲染完成也看不到。
	v.updatePageWindow(v.operation.Load())
	// 这里不依赖 updatePageWindow 是否覆盖到目标页：它带几何守卫，被守卫挡下时
	// 目标页不会被渲染。跳转自己兜底发起渲染，加载提示也只依赖这一处的结果。
	operation := v.operation.Load()
	if v.requestPageRender(operation, page) {
		if showLoading {
			v.showPageLoading(operation)
		}
	} else {
		// 目标页已经可用，不该残留上一个页面的加载提示。
		v.hidePageLoading(0)
	}
	v.updateTitle()
	v.updateControls()
	v.syncThumbnailSelection(page)
}

// scrollByViewport 按一个视口高度翻页，并吸附到页边界，避免停在两页之间。
// ScrollToOffset 在目标偏移合法时不会触发 OnScrolled，因此这里显式同步
// 当前页、缩略图高亮和显示窗口。
func (v *viewer) scrollByViewport(direction int) {
	bounds := v.pageLayout.pageBounds
	height := v.pageScroll.Size().Height
	if len(bounds) == 0 || height <= 0 {
		v.changePage(direction)
		return
	}
	target := v.pageScroll.Offset.Y + float32(direction)*height
	var page int
	if direction < 0 {
		page = pageTopAtOrBefore(bounds, target)
	} else {
		page = pageTopAtOrAfter(bounds, target)
	}
	if page < 0 {
		page = 0
	}
	if page >= len(bounds) {
		page = len(bounds) - 1
	}
	if page == v.currentPage {
		// 已经位于边界页，退化为按整页翻页。
		v.changePage(direction)
		return
	}
	v.goToPage(page, false)
}

// pageTopAtOrBefore 返回顶部位置不大于 y 的最后一页下标，没有则返回 -1。
func pageTopAtOrBefore(bounds []pageBound, y float32) int {
	return sort.Search(len(bounds), func(i int) bool {
		return bounds[i].position.Y > y
	}) - 1
}

// pageTopAtOrAfter 返回顶部位置不小于 y 的第一页下标，越界时返回 len(bounds)。
func pageTopAtOrAfter(bounds []pageBound, y float32) int {
	return sort.Search(len(bounds), func(i int) bool {
		return bounds[i].position.Y >= y
	})
}

func (v *viewer) scrollToPage(page int) {
	if page < 0 || page >= len(v.pageLayout.pageBounds) {
		return
	}
	bound := v.pageLayout.pageBounds[page]
	v.pageScroll.ScrollToOffset(fyne.NewPos(0, bound.position.Y))
}

// pageAtCenter 返回覆盖 centerY 的页面下标。页面边界按纵向位置升序排列且
// 互不重叠，因此可以二分定位；centerY 落在页间空隙时归属下一页。
// 视图中心越过最后一页时归属最后一页。
func pageAtCenter(bounds []pageBound, centerY float32) int {
	index := sort.Search(len(bounds), func(i int) bool {
		return bounds[i].position.Y+bounds[i].size.Height >= centerY
	})
	if index >= len(bounds) {
		index = len(bounds) - 1
	}
	return index
}

func (v *viewer) syncCurrentPage() {
	if v.loading || len(v.pageLayout.pageBounds) == 0 {
		return
	}
	page := pageAtCenter(v.pageLayout.pageBounds, v.pageScroll.Offset.Y+v.pageScroll.Size().Height/2)
	if page < 0 || page == v.currentPage {
		return
	}
	v.currentPage = page
	v.pageEntry.SetText(strconv.Itoa(page + 1))
	v.updateTitle()
	v.updateControls()
	v.syncThumbnailSelection(page)
}

// syncThumbnailSelection 让缩略图列表的高亮跟随当前页。列表选中本身会触发
// OnSelected，因此用 syncingThumbnail 标记挡住回环。
func (v *viewer) syncThumbnailSelection(page int) {
	if v.thumbnailList == nil || !v.thumbnailPanel.Visible() {
		return
	}
	row := v.thumbnailRow(page)
	if row < 0 || row >= v.thumbnailRowCount() {
		return
	}
	if row == v.thumbnailSelected {
		return
	}
	v.thumbnailSelected = row
	v.syncingThumbnail.Store(true)
	v.thumbnailList.Select(row)
	v.thumbnailList.ScrollTo(row)
	v.syncingThumbnail.Store(false)
}

// showPageLoading 显示页面加载提示，并附加一个兜底超时。加载提示唯一的关闭
// 入口是渲染任务回调，一旦任务丢失或卡住，弹窗就会永久遮挡界面，因此这里
// 用序号让过期定时器失效，并保证超时后一定会隐藏。
func (v *viewer) showPageLoading(operation uint64) {
	v.pageLoadingOp = operation
	if v.pageLoading != nil {
		// 弹窗已经在了，只续上兜底超时：旧定时器覆盖的是上一个请求，
		// 让它按期触发会在新请求还在渲染时提前撤掉提示。
		v.armPageLoadingTimeout()
		return
	}
	progress := widget.NewProgressBarInfinite()
	title := widget.NewLabelWithStyle("加载中", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	message := widget.NewLabel("正在加载页面，请稍候...")
	content := container.NewVBox(title, message, progress)
	content = container.NewPadded(content)
	v.pageLoading = widget.NewModalPopUp(content, v.window.Canvas())
	v.pageLoading.Show()

	v.armPageLoadingTimeout()
}

// armPageLoadingTimeout 重新设定加载提示的兜底超时。序号让已经排队的超时
// 回调失效：Stop 只能拦住尚未触发的定时器。
func (v *viewer) armPageLoadingTimeout() {
	if v.pageLoadingStop != nil {
		v.pageLoadingStop.Stop()
		v.pageLoadingStop = nil
	}
	sequence := v.pageLoadingSeq.Add(1)
	v.pageLoadingStop = time.AfterFunc(pageLoadingTimeout, func() {
		fyne.Do(func() {
			// 序号不匹配说明弹窗已经关闭并重建，这次超时属于过期回调。
			if v.closed.Load() || sequence != v.pageLoadingSeq.Load() {
				return
			}
			slog.Warn("页面加载超时，自动关闭加载提示", "page", v.currentPage+1)
			// 回调自身不能碰 pageLoadingStop：那条语句和 AfterFunc 的 goroutine
			// 没有先后关系，同时读写会构成数据竞争。
			v.dismissPageLoading(0)
		})
	})
}

// hidePageLoading 关闭加载提示并取消兜底定时器，只能在 Fyne 事件线程调用。
// 先判断能否关闭再停定时器：反过来的话，operation 不匹配时会解除兜底
// 超时却不隐藏提示，弹窗就永久留在界面上了。
func (v *viewer) hidePageLoading(operation uint64) {
	if v.pageLoading == nil {
		return
	}
	if operation != 0 && operation != v.pageLoadingOp {
		return
	}
	if v.pageLoadingStop != nil {
		v.pageLoadingStop.Stop()
		v.pageLoadingStop = nil
	}
	v.dismissPageLoading(0)
}

// dismissPageLoading 隐藏加载提示。它刻意不读写 pageLoadingStop，因此从兜底
// 定时器的回调里调用也是安全的：pageLoadingStop 只属于 Fyne 事件线程。
func (v *viewer) dismissPageLoading(operation uint64) {
	if v.pageLoading == nil {
		return
	}
	if operation != 0 && operation != v.pageLoadingOp {
		return
	}
	// 隐藏也算一次失效，堵住 Stop 没能拦下的超时回调。
	v.pageLoadingSeq.Add(1)
	v.pageLoading.Hide()
	v.pageLoading = nil
	v.pageLoadingOp = 0
}

func (v *viewer) jumpToPage() {
	if v.loading || !v.hasPages() || v.totalPages == 0 {
		return
	}
	page, err := strconv.Atoi(strings.TrimSpace(v.pageEntry.Text))
	if err != nil || page < 1 || page > v.totalPages {
		v.pageEntry.SetText(strconv.Itoa(v.currentPage + 1))
		return
	}
	v.goToPage(page-1, true)
}

func (v *viewer) setThumbnailVisible(visible bool) {
	if visible {
		v.thumbnailPanel.Show()
		v.thumbnailToggle.Importance = widget.HighImportance
	} else {
		v.thumbnailPanel.Hide()
		v.thumbnailToggle.Importance = widget.LowImportance
	}
	v.thumbnailToggle.Refresh()
	v.documentArea.Refresh()
	// 面板隐藏期间不会同步高亮，重新显示时要补上。
	v.syncThumbnailSelection(v.currentPage)
}

func (v *viewer) setViewMode(selected string) {
	mode := viewFitPage
	switch selected {
	case viewFitWidthLabel:
		mode = viewFitWidth
	case viewFitHeightLabel:
		mode = viewFitHeight
	case viewDoublePageLabel:
		mode = viewDoublePage
	}
	v.pageLayout.setMode(mode)
	if mode == viewFitHeight {
		v.pageScroll.Direction = container.ScrollBoth
	} else {
		v.pageScroll.Direction = container.ScrollVerticalOnly
	}
	v.pageLayout.refresh()
	v.pageContent.Refresh()
	v.pageScroll.Refresh()
	v.thumbnailList.Refresh()
	v.scrollToPage(v.currentPage)
	v.updatePageWindow(v.operation.Load())
	// 单页/双页的行号映射不同，切换后高亮必须重算，否则会停在另一页上。
	v.syncThumbnailSelection(v.currentPage)
}

func (v *viewer) updateControls() {
	if v.loading || v.exporting {
		v.openButton.Disable()
		v.pageEntry.Disable()
		v.pageToolbar.Hide()
		return
	}
	v.openButton.Enable()
	v.pageEntry.Enable()
	if !v.hasPages() || v.totalPages == 0 {
		v.pageToolbar.Hide()
		v.documentTitle.SetText("未加载文档")
		v.pageLabel.SetText("/ 未加载文档")
		v.pageEntry.SetText("")
		return
	}
	v.pageToolbar.Show()
	v.pageLabel.SetText(fmt.Sprintf("/ %d", v.totalPages))
	if v.pageEntry.Text != strconv.Itoa(v.currentPage+1) {
		v.pageEntry.SetText(strconv.Itoa(v.currentPage + 1))
	}
}

func documentTitle(ofd *parser.OFD, fileName string) string {
	if ofd != nil {
		for _, body := range ofd.DocBodies {
			if body.DocInfo.Title != nil {
				if title := strings.TrimSpace(*body.DocInfo.Title); title != "" {
					return title
				}
			}
		}
	}
	if title := strings.TrimSpace(fileName); title != "" {
		return title
	}
	return "未加载文档"
}

func (v *viewer) updateTitle() {
	if v.filePath == "" {
		v.window.SetTitle(applicationTitle())
		return
	}
	fileName := v.fileName
	if fileName == "" {
		fileName = filepath.Base(v.filePath)
	}
	v.window.SetTitle(fmt.Sprintf("%s - %s", fileName, applicationTitle()))
}

func applicationTitle() string {
	return "OFD Viewer on " + platformName()
}

func platformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func (v *viewer) close() {
	if v.closed.Swap(true) {
		return
	}
	v.operation.Add(1)
	v.thumbnailGeneration.Add(1)
	v.closeDocument()
}

func (v *viewer) closeDocumentOrExit() {
	if v.closed.Load() || v.loading || v.exporting {
		return
	}
	if v.ofd != nil || v.hasPages() {
		v.operation.Add(1)
		v.thumbnailGeneration.Add(1)
		v.closeDocument()
		return
	}
	v.exitApplication()
}

func (v *viewer) exitApplication() {
	v.close()
	if driver, ok := fyne.CurrentApp().Driver().(mobile.Driver); ok {
		driver.GoBack()
		// Android 的 Activity 结束后进程可能仍被系统保留，重新启动时会复用
		// 已结束的 Fyne 状态。资源已在 v.close 中释放，因此这里确保进程退出。
		if runtime.GOOS == "android" {
			os.Exit(0)
		}
		return
	}
	v.window.Close()
}

func (v *viewer) closeDocument() {
	// 关闭路径都会推进 operation，在途渲染的 hidePageLoading 因此不会执行。
	// 不在这里收起提示，模态框会一直挡在界面上直到兜底超时。
	v.hidePageLoading(0)
	// 只标记会话退休，底层文档在后台释放，避免关闭文档时阻塞事件线程。
	v.session.retire()
	v.session = nil
	v.ofd = nil
	v.documents = nil
	v.pages = nil
	v.pageSlots = nil
	v.pageContent.Objects = nil
	// 排版状态和显示帧整体丢弃：帧只服务于当前文档，重新按需创建更简单。
	v.pageLayout.setPages(nil)
	v.totalPages = 0
	v.currentPage = 0
	v.maxPageArea = 0
	// 槽位已经清空，缓存条目无法再回填，顺带释放已渲染图像占用的内存。
	v.pageImages = v.newPageImageCache()
	v.thumbnails = newThumbnailCache()
	v.thumbnailRendering = nil
	v.thumbnailSelected = -1
	v.filePath = ""
	v.fileName = ""
	v.documentTitle.SetText("未加载文档")
	v.pageContent.Refresh()
	v.pageScroll.Refresh()
	v.thumbnailList.Refresh()
	v.updateTitle()
	v.updateControls()
}

func validOFDFile(filePath string) string {
	if !strings.HasSuffix(strings.ToLower(filePath), ".ofd") {
		return ""
	}
	if _, err := os.Stat(filePath); err != nil {
		return ""
	}
	return filePath
}
