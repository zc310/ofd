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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fyneCanvas "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ofdcanvas "github.com/tdewolff/canvas"
	canvasFyne "github.com/tdewolff/canvas/renderers/fyne"
	canvasPDF "github.com/tdewolff/canvas/renderers/pdf"
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
		viewer.load(initialFile, filepath.Base(initialFile), "")
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
	exportButton    *widget.Button
	pageLabel       *widget.Label
	pageEntry       *widget.Entry
	jumpButton      *widget.Button
	thumbnailToggle *widget.Check
	viewModeSelect  *widget.Select
	infoButton      *widget.Button
	pageLoading     *widget.PopUp
	pageLoadingOp   uint64
	exportLoading   *widget.PopUp
	thumbnailPanel  *fyne.Container
	documentArea    *container.Split

	filePath            string
	fileName            string
	temporaryFile       string
	ofd                 *parser.OFD
	doc                 *render.Document
	currentPage         int
	totalPages          int
	loading             bool
	exporting           bool
	operation           atomic.Uint64
	thumbnailGeneration atomic.Uint64
	thumbnailImages     map[int]image.Image
	thumbnailRendering  []atomic.Bool
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

	v.openButton = widget.NewButton("打开 OFD (O)", func() {
		v.chooseFile()
	})
	v.exportButton = widget.NewButton("导出", func() {
		v.showExportDialog()
	})
	v.pageLabel = widget.NewLabel("/ 未加载文档")
	v.pageEntry = widget.NewEntry()
	v.pageEntry.SetPlaceHolder("页码")
	v.jumpButton = widget.NewButton("跳转", func() {
		v.jumpToPage()
	})
	v.pageEntry.OnSubmitted = func(string) {
		v.jumpToPage()
	}
	v.thumbnailToggle = widget.NewCheck("显示缩略图", func(checked bool) {
		v.setThumbnailVisible(checked)
	})
	v.thumbnailToggle.Checked = false
	v.viewModeSelect = widget.NewSelect([]string{viewFitPageLabel, viewFitWidthLabel, viewFitHeightLabel, viewDoublePageLabel}, func(selected string) {
		v.setViewMode(selected)
	})
	v.viewModeSelect.SetSelected(viewFitWidthLabel)
	v.infoButton = widget.NewButtonWithIcon("", theme.InfoIcon(), func() {
		v.showAppInfo()
	})
	v.infoButton.Importance = widget.LowImportance

	toolbarContent := container.NewHBox(
		v.openButton,
		v.exportButton,
		widget.NewSeparator(),
		v.pageEntry,
		v.pageLabel,
		v.jumpButton,
		widget.NewSeparator(),
		widget.NewLabel("视图:"),
		v.viewModeSelect,
		widget.NewSeparator(),
		v.thumbnailToggle,
	)
	toolbar := container.NewBorder(nil, nil, nil, v.infoButton, toolbarContent)
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

func (v *viewer) showExportDialog() {
	if v.doc == nil || v.totalPages == 0 || v.loading || v.exporting {
		return
	}
	dpiEntry := widget.NewEntry()
	dpiEntry.SetText(strconv.Itoa(exportDPI))
	formatSelect := widget.NewSelect([]string{
		exportFormatPDF,
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
	if v.doc == nil || v.exporting {
		return
	}
	extension := "pdf"
	if !strings.EqualFold(format, "PDF") && v.totalPages > 1 {
		extension = "zip"
	} else if !strings.EqualFold(format, "PDF") {
		extension = strings.ToLower(format)
	}
	fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowInformation("导出失败", err.Error(), v.window)
			return
		}
		if writer == nil {
			return
		}
		v.exportToWriter(writer, format, dpi, background, extension)
	}, v.window)
	fileDialog.SetTitleText("导出 OFD")
	fileDialog.SetFileName(strings.TrimSuffix(v.fileName, filepath.Ext(v.fileName)) + "." + extension)
	fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{"." + extension}))
	fileDialog.SetConfirmText("导出")
	fileDialog.SetDismissText("取消")
	fileDialog.Show()
}

func (v *viewer) exportToWriter(writer fyne.URIWriteCloser, format string, dpi int, background color.Color, extension string) {
	v.exporting = true
	v.updateControls()
	v.showExportLoading()
	doc := v.doc
	go func() {
		var temporaryFile *os.File
		var temporaryPath string
		var err error
		temporaryFile, err = os.CreateTemp("", "ofd-export-*."+extension)
		if err == nil {
			temporaryPath = temporaryFile.Name()
			err = temporaryFile.Close()
		}
		if err == nil {
			v.renderMu.Lock()
			err = exportDocument(doc, temporaryPath, format, dpi, background)
			v.renderMu.Unlock()
		}
		if err == nil {
			var input *os.File
			input, err = os.Open(temporaryPath)
			if err == nil {
				_, err = io.Copy(writer, input)
				closeErr := input.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		closeErr := writer.Close()
		if err == nil {
			err = closeErr
		}
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
		fyne.Do(func() {
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

func exportDocument(doc *render.Document, path, format string, dpi int, background color.Color) error {
	exportDoc := render.NewDocument(background, doc.Document)
	if strings.EqualFold(format, "pdf") {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		var pdfDoc *canvasPDF.PDF
		for i, page := range exportDoc.Pages {
			canvasPage, err := exportDoc.Page(page)
			if err != nil {
				return fmt.Errorf("处理第 %d 页失败: %w", i+1, err)
			}
			if pdfDoc == nil {
				pdfDoc = canvasPDF.New(file, canvasPage.W, canvasPage.H, nil)
			} else {
				pdfDoc.NewPage(canvasPage.W, canvasPage.H)
			}
			canvasPage.RenderTo(pdfDoc)
		}
		if pdfDoc == nil {
			return fmt.Errorf("文档没有页面")
		}
		return pdfDoc.Close()
	}
	option := exportImageOption(format)
	if len(exportDoc.Pages) == 1 {
		return canvasConverter.ImageDocument(exportDoc,
			canvasConverter.DPI(float64(dpi)),
			option,
			canvasConverter.Page(1),
			canvasConverter.Writer(func(int) (io.WriteCloser, error) {
				return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			}),
		)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	extension := strings.ToLower(format)
	err = canvasConverter.ImageDocument(exportDoc,
		canvasConverter.DPI(float64(dpi)),
		option,
		canvasConverter.Writer(func(page int) (io.WriteCloser, error) {
			entry, err := archive.Create(fmt.Sprintf("page-%04d.%s", page, extension))
			if err != nil {
				return nil, err
			}
			return &zipEntryWriter{Writer: entry}, nil
		}),
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
	if !strings.EqualFold(format, "pdf") && pages > 1 {
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

func (v *viewer) showAppInfo() {
	link, err := url.Parse(projectURL)
	if err != nil {
		return
	}
	content := container.NewVBox(
		widget.NewLabelWithStyle("OFD Viewer", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("版本: "+applicationVersion),
		widget.NewLabel("OFD 文档查看器"),
		widget.NewLabel("应用 ID: "+applicationID),
		widget.NewLabel("项目地址:"),
		widget.NewHyperlink(projectURL, link),
	)
	dialog.NewCustom("关于 OFD Viewer", "关闭", content, v.window).Show()
}

func (v *viewer) createPageSlots(doc *render.Document) {
	v.pageSlots = make([]*pageSlot, len(doc.Pages))
	v.pageLayout.slots = v.pageSlots
	objects := make([]fyne.CanvasObject, len(doc.Pages))
	for i, page := range doc.Pages {
		page.EnsurePhysicalBox()
		box := page.Area.PhysicalBox
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
	if v.loading {
		return
	}
	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowInformation("打开失败", err.Error(), v.window)
			return
		}
		if reader == nil {
			return
		}
		v.loading = true
		v.updateControls()
		fileName := reader.URI().Name()
		go func() {
			temporaryFile, copyErr := os.CreateTemp("", "ofd-viewer-*.ofd")
			var temporaryPath string
			if copyErr == nil {
				temporaryPath = temporaryFile.Name()
				_, copyErr = io.Copy(temporaryFile, reader)
				closeErr := temporaryFile.Close()
				if copyErr == nil {
					copyErr = closeErr
				}
			}
			closeErr := reader.Close()
			if copyErr == nil {
				copyErr = closeErr
			}
			if copyErr != nil && temporaryPath != "" {
				_ = os.Remove(temporaryPath)
			}
			fyne.Do(func() {
				if copyErr != nil {
					v.loading = false
					v.updateControls()
					dialog.ShowInformation("打开失败", copyErr.Error(), v.window)
					return
				}
				v.load(temporaryPath, fileName, temporaryPath)
			})
		}()
	}, v.window)
	fileDialog.SetTitleText("选择 OFD 文件")
	fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".ofd"}))
	fileDialog.SetConfirmText("打开")
	fileDialog.SetDismissText("取消")
	fileDialog.Show()
}

func (v *viewer) load(filePath, fileName, temporaryFile string) {
	v.hidePageLoading(0)
	operation := v.operation.Add(1)
	v.thumbnailGeneration.Add(1)
	v.loading = true
	v.updateControls()

	go func() {
		log.Printf("正在打开文件: %s", filePath)
		ofd, err := parser.NewOFD(filePath)
		if err == nil && len(ofd.Documents) == 0 {
			_ = ofd.Close()
			err = fmt.Errorf("没有文档")
		}

		fyne.Do(func() {
			if operation != v.operation.Load() {
				if ofd != nil {
					_ = ofd.Close()
				}
				if temporaryFile != "" {
					_ = os.Remove(temporaryFile)
				}
				return
			}
			if err != nil {
				if ofd != nil {
					_ = ofd.Close()
				}
				if temporaryFile != "" {
					_ = os.Remove(temporaryFile)
				}
				v.loading = false
				log.Printf("打开失败: %v", err)
				v.updateControls()
				return
			}

			v.closeDocument()
			v.ofd = ofd
			v.doc = render.NewDocument(color.Transparent, ofd.Documents[0])
			v.filePath = filePath
			v.fileName = fileName
			v.temporaryFile = temporaryFile
			v.currentPage = 0
			v.totalPages = len(v.doc.Pages)
			v.pageEntry.SetText("1")
			v.thumbnailImages = make(map[int]image.Image)
			v.thumbnailRendering = make([]atomic.Bool, v.totalPages)
			v.createPageSlots(v.doc)
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
	if operation == 0 || v.doc == nil || len(v.pageLayout.pageBounds) != len(v.pageSlots) {
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
	if operation == 0 || v.doc == nil || pageIndex < 0 || pageIndex >= len(v.pageSlots) {
		return
	}
	slot := v.pageSlots[pageIndex]
	if slot.image.Image != nil || !slot.rendering.CompareAndSwap(false, true) {
		return
	}
	doc := v.doc
	page := doc.Pages[pageIndex]
	page.EnsurePhysicalBox()
	go v.renderPage(operation, doc, pageIndex, page, slot)
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
	if generation == 0 || v.doc == nil || pageIndex < 0 || pageIndex >= len(v.doc.Pages) || pageIndex >= len(v.thumbnailRendering) {
		return
	}
	if v.thumbnailImages[pageIndex] != nil || !v.thumbnailRendering[pageIndex].CompareAndSwap(false, true) {
		return
	}
	doc := v.doc
	page := doc.Pages[pageIndex]
	page.EnsurePhysicalBox()
	go func() {
		img, err := v.renderPageImage(doc, page, ofdcanvas.DPI(thumbnailDPI), func() bool {
			return generation == v.thumbnailGeneration.Load()
		})
		v.thumbnailRendering[pageIndex].Store(false)
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

func (v *viewer) renderPageImage(doc *render.Document, page *parser.Page, resolution ofdcanvas.Resolution, valid func() bool) (image.Image, error) {
	v.renderMu.Lock()
	defer v.renderMu.Unlock()
	if !valid() {
		return nil, nil
	}
	page.EnsurePhysicalBox()
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
	if v.loading || v.doc == nil || v.totalPages == 0 {
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
		v.window.Close()
	default:
		switch strings.ToLower(string(event.Name)) {
		case "o":
			v.chooseFile()
		case "a", "w", "s":
			v.changePage(-1)
		case "d":
			v.changePage(1)
		case "q":
			v.window.Close()
		}
	}
}

func (v *viewer) goToPage(page int, showLoading bool) {
	if v.loading || v.doc == nil || page < 0 || page >= v.totalPages {
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
	if v.loading || v.doc == nil || v.totalPages == 0 {
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
	} else {
		v.thumbnailPanel.Hide()
	}
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
		v.exportButton.Disable()
		v.pageEntry.Disable()
		v.jumpButton.Disable()
		v.viewModeSelect.Disable()
		return
	}
	v.openButton.Enable()
	if v.doc == nil || v.totalPages == 0 || v.exporting {
		v.exportButton.Disable()
	} else {
		v.exportButton.Enable()
	}
	v.pageEntry.Enable()
	v.jumpButton.Enable()
	v.viewModeSelect.Enable()
	if v.doc == nil || v.totalPages == 0 {
		v.pageLabel.SetText("/ 未加载文档")
		v.pageEntry.SetText("")
		return
	}
	v.pageLabel.SetText(fmt.Sprintf("/ %d", v.totalPages))
	if v.pageEntry.Text != strconv.Itoa(v.currentPage+1) {
		v.pageEntry.SetText(strconv.Itoa(v.currentPage + 1))
	}
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
	if runtime.GOOS == "windows" {
		v.window.SetTitle(fmt.Sprintf("%s - 第 %d/%d 页 - %s", fileName, v.currentPage+1, v.totalPages, applicationTitle()))
		return
	}
	v.window.SetTitle(fmt.Sprintf("%s - %d/%d - %s", fileName, v.currentPage+1, v.totalPages, applicationTitle()))
}

func applicationTitle() string {
	return "OFD Viewer " + applicationVersion + " on " + platformName()
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
	v.operation.Add(1)
	v.thumbnailGeneration.Add(1)
	v.closeDocument()
}

func (v *viewer) closeDocument() {
	v.renderMu.Lock()
	defer v.renderMu.Unlock()
	if v.ofd != nil {
		_ = v.ofd.Close()
		v.ofd = nil
	}
	if v.temporaryFile != "" {
		_ = os.Remove(v.temporaryFile)
		v.temporaryFile = ""
	}
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
