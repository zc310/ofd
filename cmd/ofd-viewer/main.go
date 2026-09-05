package main

import (
	"archive/zip"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fyneCanvas "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ofdcanvas "github.com/tdewolff/canvas"
	canvasFyne "github.com/tdewolff/canvas/renderers/fyne"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	canvasConverter "github.com/zc310/ofd/pkg/converter"
)

const (
	applicationID      = "io.github.zc310.ofd"
	applicationVersion = "v0.0.5"
	projectURL         = "https://github.com/zc310/ofd"
	windowWidth        = 800
	windowHeight       = 600
	viewerDPI          = 96
	thumbnailDPI       = 36
	exportDPI          = 150
)

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

func main() {
	initialFile := validOFDFile("")
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
	pageSlots       []*pageSlot
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
	exportLoading   *widget.PopUp
	thumbnailPanel  *fyne.Container
	documentArea    *container.Split

	filePath            string
	fileName            string
	ofd                 *parser.OFD
	documents           []*render.Document
	pages               []viewerPage
	currentPage         int
	totalPages          int
	loading             bool
	exporting           bool
	operation           atomic.Uint64
	thumbnailGeneration atomic.Uint64
	thumbnailImages     map[int]image.Image
	thumbnailRendering  []atomic.Bool
	closed              atomic.Bool
	renderMu            sync.Mutex
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

type pageSlot struct {
	frame     *fyne.Container
	image     *fyneCanvas.Image
	aspect    float32
	rendering atomic.Bool
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
	slots      []*pageSlot
	pageBounds []pageBound
	pageScroll *container.Scroll
	gap        float32
	margin     float32
	minSize    fyne.Size
	viewport   fyne.Size
}

func (l *continuousLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	viewport := l.viewport
	if l.pageScroll != nil && l.pageScroll.Size().Width > 0 && l.pageScroll.Size().Height > 0 {
		viewport = l.pageScroll.Size()
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		viewport = size
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}

	pageSizes := make([]fyne.Size, len(objects))
	contentWidth := viewport.Width
	if l.mode == viewDoublePage {
		contentHeight := l.margin
		for i := range objects {
			pageSizes[i] = l.pageSize(l.pageAspect(i), viewport)
		}
		l.pageBounds = make([]pageBound, len(objects))
		for rowStart := 0; rowStart < len(objects); rowStart += 2 {
			rowHeight := pageSizes[rowStart].Height
			if rowStart+1 < len(objects) && pageSizes[rowStart+1].Height > rowHeight {
				rowHeight = pageSizes[rowStart+1].Height
			}
			contentHeight += rowHeight
			if rowStart+2 < len(objects) {
				contentHeight += l.gap
			}
		}
		contentHeight += l.margin
		l.minSize = fyne.NewSize(contentWidth, contentHeight)
		l.viewport = viewport

		y := l.margin
		for rowStart := 0; rowStart < len(objects); rowStart += 2 {
			rowEnd := rowStart + 2
			if rowEnd > len(objects) {
				rowEnd = len(objects)
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
				objects[i].Move(pagePosition)
				objects[i].Resize(pageSizes[i])
				l.pageBounds[i] = pageBound{position: pagePosition, size: pageSizes[i]}
				x += pageSizes[i].Width + l.gap
			}
			y += rowHeight + l.gap
		}
		return
	}
	contentHeight := l.margin
	for i := range objects {
		pageSizes[i] = l.pageSize(l.pageAspect(i), viewport)
		if pageSizes[i].Width+2*l.margin > contentWidth {
			contentWidth = pageSizes[i].Width + 2*l.margin
		}
		contentHeight += pageSizes[i].Height
		if i < len(objects)-1 {
			contentHeight += l.gap
		}
	}
	contentHeight += l.margin
	l.minSize = fyne.NewSize(contentWidth, contentHeight)
	l.viewport = viewport
	l.pageBounds = make([]pageBound, len(objects))

	y := l.margin
	for i, object := range objects {
		sz := pageSizes[i]
		x := (contentWidth - sz.Width) / 2
		position := fyne.NewPos(x, y)
		object.Move(position)
		object.Resize(sz)
		l.pageBounds[i] = pageBound{position: position, size: sz}
		y += sz.Height + l.gap
	}
}

func (l *continuousLayout) pageAspect(index int) float32 {
	if index >= 0 && index < len(l.slots) && l.slots[index].aspect > 0 {
		return l.slots[index].aspect
	}
	return 1
}

func (l *continuousLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	if l.minSize.Width > 0 && l.minSize.Height > 0 {
		return l.minSize
	}
	return fyne.NewSize(1, 1)
}

func (l *continuousLayout) refresh() {
	l.minSize = fyne.Size{}
	if l.pageScroll != nil {
		l.pageScroll.Refresh()
	}
}

func (l *continuousLayout) setMode(mode pageViewMode) {
	l.mode = mode
	l.minSize = fyne.Size{}
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

type thumbnailCell struct {
	widget.BaseWidget
	content *fyne.Container
	image   *fyneCanvas.Image
	label   *widget.Label
	onTap   func()
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

func (c *thumbnailCell) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
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
	cell.onTap = func() {
		v.goToPage(page, true)
	}
	cell.label.SetText(fmt.Sprintf("第 %d 页", page+1))
	if img := v.thumbnailImages[page]; img != nil {
		cell.image.Image = img
	} else {
		cell.image.Image = nil
		v.requestThumbnailRender(page)
	}
	cell.image.Refresh()
}

func newViewer(window fyne.Window) *viewer {
	v := &viewer{window: window}
	v.thumbnailImages = make(map[int]image.Image)
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
			firstPage := int(id)
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
		page := int(id)
		if v.isDoublePage() {
			page *= 2
		}
		v.goToPage(page, true)
	}

	v.pageLayout = &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	v.pageContent = container.New(v.pageLayout)
	v.pageScroll = container.NewScroll(v.pageContent)
	v.pageScroll.Direction = container.ScrollVerticalOnly
	v.pageLayout.pageScroll = v.pageScroll
	v.pageScroll.OnScrolled = func(fyne.Position) {
		v.syncCurrentPage()
		v.renderVisiblePages(v.operation.Load())
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
	dpiEntry := widget.NewEntry()
	dpiEntry.SetText(strconv.Itoa(exportDPI))
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
	backgroundSelect := widget.NewSelect([]string{exportBackgroundTransparent, exportBackgroundWhite}, nil)
	backgroundSelect.SetSelected(exportBackgroundTransparent)
	content := widget.NewForm(
		widget.NewFormItem("DPI", dpiEntry),
		widget.NewFormItem("格式", formatSelect),
		widget.NewFormItem("背景颜色", backgroundSelect),
	)
	exportDialog := dialog.NewCustomConfirm("导出文档", "导出", "取消", content, func(confirmed bool) {
		if !confirmed {
			return
		}
		dpi, err := strconv.Atoi(strings.TrimSpace(dpiEntry.Text))
		if err != nil || dpi < 1 || dpi > 1200 {
			dialog.ShowInformation("导出失败", "DPI 必须是 1-1200 之间的整数。", v.window)
			return
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

func (v *viewer) export(format string, dpi int, background color.Color) {
	if v.closed.Load() || v.loading || !v.hasPages() || v.exporting {
		return
	}
	extension := "pdf"
	if strings.EqualFold(format, "txt") {
		extension = "txt"
	} else if !strings.EqualFold(format, "PDF") && v.totalPages > 1 {
		extension = "zip"
	} else if !strings.EqualFold(format, "PDF") {
		extension = strings.ToLower(format)
	}
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
				v.exporting = false
				v.updateControls()
				dialog.ShowInformation("导出失败", err.Error(), v.window)
				return
			}
			if selection.path == "" && selection.output == nil {
				v.exporting = false
				v.updateControls()
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
		return
	}
	v.exportToOutput(func() (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}, nil, format, dpi, background)
}

func (v *viewer) exportToWriter(writer io.WriteCloser, format string, dpi int, background color.Color) {
	if writer == nil {
		return
	}
	v.exportToOutput(func() (io.WriteCloser, error) {
		return writer, nil
	}, writer, format, dpi, background)
}

func (v *viewer) exportToOutput(create func() (io.WriteCloser, error), pending io.WriteCloser, format string, dpi int, background color.Color) {
	if v.closed.Load() || v.loading || !v.hasPages() {
		if pending != nil {
			_ = pending.Close()
		}
		return
	}
	v.showExportLoading()
	exportOperation := v.operation.Load()
	v.renderMu.Lock()
	documents := append([]*render.Document(nil), v.documents...)
	v.renderMu.Unlock()
	go func() {
		v.renderMu.Lock()
		var err error
		if exportOperation != v.operation.Load() {
			err = fmt.Errorf("文档已更改")
			if pending != nil {
				_ = pending.Close()
			}
		} else {
			var writer io.WriteCloser
			writer, err = create()
			if err == nil {
				err = exportDocumentsToWriter(documents, writer, format, dpi, background)
				if closeErr := writer.Close(); err == nil {
					err = closeErr
				}
			}
		}
		v.renderMu.Unlock()
		fyne.Do(func() {
			if v.closed.Load() {
				return
			}
			v.hideExportLoading()
			v.exporting = false
			v.updateControls()
			if err != nil {
				dialog.ShowInformation("导出失败", err.Error(), v.window)
				return
			}
			dialog.ShowInformation("导出完成", "文件已成功导出。", v.window)
		})
	}()
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

func exportDocuments(documents []*render.Document, path, format string, dpi int, background color.Color) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	return exportDocumentsToWriter(documents, file, format, dpi, background)
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
		exportDoc := render.NewDocument(background, doc.Document)
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
				return &nonClosingWriter{Writer: output}, nil
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
				return &zipEntryWriter{Writer: entry}, nil
			}))...,
	)
	if err != nil {
		archive.Close()
		return err
	}
	return archive.Close()
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

func exportFileTypeName(format string, pages int) string {
	if !strings.EqualFold(format, "pdf") && !strings.EqualFold(format, "txt") && pages > 1 {
		return "ZIP 压缩包"
	}
	return strings.ToUpper(format) + " 文件"
}

type zipEntryWriter struct {
	io.Writer
}

func (w *zipEntryWriter) Close() error {
	return nil
}

type nonClosingWriter struct {
	io.Writer
}

func (w *nonClosingWriter) Close() error {
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

func (v *viewer) createPageSlots(pages []viewerPage) {
	v.pageSlots = make([]*pageSlot, len(pages))
	v.pageLayout.slots = v.pageSlots
	objects := make([]fyne.CanvasObject, len(pages))
	for i, pageRef := range pages {
		pageRef.page.EnsurePhysicalBox()
		box := pageRef.page.Area.PhysicalBox
		aspect := float32(1)
		if box.Height > 0 {
			aspect = float32(box.Width / box.Height)
		}
		background := fyneCanvas.NewRectangle(color.White)
		pageImage := fyneCanvas.NewImageFromImage(nil)
		pageImage.FillMode = fyneCanvas.ImageFillContain
		pageImage.ScaleMode = fyneCanvas.ImageScaleSmooth
		frame := container.NewStack(background, pageImage)
		v.pageSlots[i] = &pageSlot{frame: frame, image: pageImage, aspect: aspect}
		objects[i] = frame
	}
	v.pageContent.Objects = objects
	v.pageLayout.minSize = fyne.Size{}
	v.pageLayout.pageBounds = nil
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

func (v *viewer) load(filePath, fileName string, input interface{}) {
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

	go func() {
		log.Printf("正在打开文件: %s", filePath)
		ofd, err := openOFD(input)
		if closeErr := closeInput(input); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
		if err == nil && ofd == nil {
			err = fmt.Errorf("打开 OFD 失败: 解析器为空")
		}
		if err == nil && len(ofd.Documents) == 0 {
			_ = ofd.Close()
			err = fmt.Errorf("没有文档")
		}

		fyne.Do(func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					if ofd != nil {
						_ = ofd.Close()
					}
					if operation == v.operation.Load() && !v.closed.Load() {
						v.loading = false
						v.updateControls()
						err := fmt.Errorf("处理 OFD 文件失败: %v", recovered)
						log.Printf("打开 OFD panic: %v\n%s", recovered, debug.Stack())
						dialog.ShowInformation("打开失败", err.Error(), v.window)
					}
				}
			}()
			if operation != v.operation.Load() {
				if ofd != nil {
					_ = ofd.Close()
				}
				return
			}
			if v.closed.Load() {
				if ofd != nil {
					_ = ofd.Close()
				}
				return
			}
			if err != nil {
				if ofd != nil {
					_ = ofd.Close()
				}
				v.loading = false
				log.Printf("打开失败: %v", err)
				v.updateControls()
				dialog.ShowInformation("打开失败", err.Error(), v.window)
				return
			}

			v.renderMu.Lock()
			v.closeDocumentLocked()
			v.ofd = ofd
			v.documentTitle.SetText(documentTitle(ofd, fileName))
			v.documents = make([]*render.Document, 0, len(ofd.Documents))
			for _, document := range ofd.Documents {
				renderDoc := render.NewDocument(color.Transparent, document)
				v.documents = append(v.documents, renderDoc)
			}
			v.pages = collectViewerPages(v.documents)
			if len(v.pages) == 0 {
				_ = v.ofd.Close()
				v.ofd = nil
				v.documents = nil
				v.pages = nil
				v.renderMu.Unlock()
				v.loading = false
				log.Printf("打开失败: 文档没有页面")
				v.updateControls()
				return
			}
			v.renderMu.Unlock()
			v.filePath = filePath
			v.fileName = fileName
			v.currentPage = 0
			v.totalPages = len(v.pages)
			v.pageEntry.SetText("1")
			v.thumbnailImages = make(map[int]image.Image)
			v.thumbnailRendering = make([]atomic.Bool, v.totalPages)
			v.createPageSlots(v.pages)
			v.pageScroll.ScrollToTop()
			v.thumbnailList.Refresh()
			v.thumbnailList.Select(0)
			v.loading = false
			v.updateTitle()
			v.updateControls()
			v.requestPageRender(operation, v.currentPage)
			v.renderVisiblePages(operation)
		})
	}()
}

func (v *viewer) renderVisiblePages(operation uint64) {
	if operation == 0 || !v.hasPages() || len(v.pageLayout.pageBounds) != len(v.pageSlots) {
		return
	}
	top := v.pageScroll.Offset.Y
	bottom := top + v.pageScroll.Size().Height
	for pageIndex, bound := range v.pageLayout.pageBounds {
		if bound.position.Y > bottom || bound.position.Y+bound.size.Height < top {
			continue
		}
		v.requestPageRender(operation, pageIndex)
	}
}

func (v *viewer) requestPageRender(operation uint64, pageIndex int) {
	if operation == 0 || !v.hasPages() || pageIndex < 0 || pageIndex >= len(v.pageSlots) {
		return
	}
	slot := v.pageSlots[pageIndex]
	if slot.image.Image != nil || !slot.rendering.CompareAndSwap(false, true) {
		return
	}
	pageRef, ok := v.pageAt(pageIndex)
	if !ok {
		slot.rendering.Store(false)
		return
	}
	pageRef.page.EnsurePhysicalBox()
	go v.renderPage(operation, pageRef.document, pageIndex, pageRef.page, slot)
}

func (v *viewer) renderPage(operation uint64, doc *render.Document, pageIndex int, page *parser.Page, slot *pageSlot) {

	img, err := v.renderPageImage(doc, page, ofdcanvas.DPI(viewerDPI), func() bool {
		return operation == v.operation.Load()
	})
	if err != nil || img == nil {
		slot.rendering.Store(false)
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
		slot.image.Image = img
		slot.image.Refresh()
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
	if v.thumbnailImages[pageIndex] != nil || !v.thumbnailRendering[pageIndex].CompareAndSwap(false, true) {
		return
	}
	rendering := &v.thumbnailRendering[pageIndex]
	pageRef, ok := v.pageAt(pageIndex)
	if !ok {
		rendering.Store(false)
		return
	}
	pageRef.page.EnsurePhysicalBox()
	go func() {
		img, err := v.renderPageImage(pageRef.document, pageRef.page, ofdcanvas.DPI(thumbnailDPI), func() bool {
			return generation == v.thumbnailGeneration.Load()
		})
		rendering.Store(false)
		if err != nil || img == nil {
			return
		}
		fyne.Do(func() {
			if generation != v.thumbnailGeneration.Load() {
				return
			}
			v.thumbnailImages[pageIndex] = img
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
			log.Printf("解析 OFD panic: %v\n%s", recovered, debug.Stack())
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

func (v *viewer) renderPageImage(doc *render.Document, page *parser.Page, resolution ofdcanvas.Resolution, valid func() bool) (img image.Image, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			img = nil
			log.Printf("渲染页面 panic: %v\n%s", recovered, debug.Stack())
			err = fmt.Errorf("渲染页面失败: %v", recovered)
		}
	}()
	if doc == nil || doc.Document == nil || page == nil {
		return nil, fmt.Errorf("页面数据为空")
	}
	v.renderMu.Lock()
	defer v.renderMu.Unlock()
	if !valid() {
		return nil, nil
	}
	page.EnsurePhysicalBox()
	if page.Area == nil {
		return nil, fmt.Errorf("页面区域为空")
	}
	box := page.Area.PhysicalBox
	pageCanvas := canvasFyne.New(box.Width, box.Height, resolution)
	ctx := ofdcanvas.NewContext(pageCanvas.Canvas)
	if err := doc.Draw(ctx, page); err != nil {
		return nil, err
	}
	pageObject, ok := pageCanvas.Content().(*fyneCanvas.Image)
	if !ok || pageObject.Image == nil {
		return nil, nil
	}
	return pageObject.Image, nil
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
	case fyne.KeyLeft, fyne.KeyUp, fyne.KeyPageUp:
		v.changePage(-1)
	case fyne.KeyRight, fyne.KeyDown, fyne.KeyPageDown:
		v.changePage(1)
	case fyne.KeyHome:
		v.goToPage(0, false)
	case fyne.KeyEnd:
		v.goToPage(v.totalPages-1, false)
	case fyne.KeyEscape:
		v.exitApplication()
	default:
		switch strings.ToLower(string(event.Name)) {
		case "o":
			v.chooseFile()
		case "a", "w", "s":
			v.changePage(-1)
		case "d":
			v.changePage(1)
		case "q":
			v.exitApplication()
		}
	}
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
	v.requestPageRender(v.operation.Load(), page)
	if showLoading && v.pageSlots[page].image.Image == nil {
		v.showPageLoading(v.operation.Load())
	}
	v.updateTitle()
	v.updateControls()
}

func (v *viewer) scrollToPage(page int) {
	if page < 0 || page >= len(v.pageLayout.pageBounds) {
		return
	}
	bound := v.pageLayout.pageBounds[page]
	v.pageScroll.ScrollToOffset(fyne.NewPos(0, bound.position.Y))
}

func (v *viewer) syncCurrentPage() {
	if v.loading || len(v.pageLayout.pageBounds) == 0 {
		return
	}
	centerY := v.pageScroll.Offset.Y + v.pageScroll.Size().Height/2
	page := v.currentPage
	pageInCenterRow := -1
	for i, bound := range v.pageLayout.pageBounds {
		if centerY >= bound.position.Y && centerY <= bound.position.Y+bound.size.Height {
			if pageInCenterRow == -1 {
				pageInCenterRow = i
			}
			if i == v.currentPage {
				pageInCenterRow = i
				break
			}
		}
	}
	if pageInCenterRow >= 0 {
		page = pageInCenterRow
	}
	if page == v.currentPage {
		return
	}
	v.currentPage = page
	v.pageEntry.SetText(strconv.Itoa(page + 1))
	v.updateTitle()
	v.updateControls()
}

func (v *viewer) showPageLoading(operation uint64) {
	v.pageLoadingOp = operation
	if v.pageLoading != nil {
		return
	}
	progress := widget.NewProgressBarInfinite()
	title := widget.NewLabelWithStyle("加载中", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	message := widget.NewLabel("正在加载页面，请稍候...")
	content := container.NewVBox(title, message, progress)
	content = container.NewPadded(content)
	v.pageLoading = widget.NewModalPopUp(content, v.window.Canvas())
	v.pageLoading.Show()
}

func (v *viewer) hidePageLoading(operation uint64) {
	if v.pageLoading == nil || (operation != 0 && operation != v.pageLoadingOp) {
		return
	}
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
	v.renderVisiblePages(v.operation.Load())
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
		return
	}
	v.window.Close()
}

func (v *viewer) closeDocument() {
	v.renderMu.Lock()
	defer v.renderMu.Unlock()
	v.closeDocumentLocked()
}

func (v *viewer) closeDocumentLocked() {
	if v.ofd != nil {
		_ = v.ofd.Close()
		v.ofd = nil
	}
	v.documents = nil
	v.pages = nil
	v.pageSlots = nil
	v.pageContent.Objects = nil
	v.pageLayout.pageBounds = nil
	v.pageLayout.slots = nil
	v.pageLayout.minSize = fyne.Size{}
	v.totalPages = 0
	v.currentPage = 0
	v.thumbnailImages = nil
	v.thumbnailRendering = nil
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
