package main

import (
	"image"

	"fyne.io/fyne/v2"
	fyneCanvas "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// pageImage 用统一的组件接口显示栅格化后的 OFD 页面。
type pageImage struct {
	widget.BaseWidget
	image *fyneCanvas.Image
}

func newPageImage() *pageImage {
	canvasImage := fyneCanvas.NewImageFromImage(nil)
	canvasImage.FillMode = fyneCanvas.ImageFillContain
	canvasImage.ScaleMode = fyneCanvas.ImageScaleSmooth
	page := &pageImage{image: canvasImage}
	page.ExtendBaseWidget(page)
	return page
}

func (p *pageImage) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.image)
}

func (p *pageImage) SetImage(image image.Image) {
	p.image.Image = image
	p.Refresh()
}

func (p *pageImage) Clear() {
	p.image.Image = nil
	p.Refresh()
}
