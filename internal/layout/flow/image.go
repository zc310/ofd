package flow

import (
	"bytes"
	"fmt"
	"image"
	"math"

	"github.com/zc310/ofd/pkg/creator"
)

func (e *engine) prepareImage(p preparedBlock) (preparedBlock, error) {
	img := p.block.Image
	for _, v := range []float64{img.Width, img.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return p, fmt.Errorf("图片尺寸必须为有限非负数")
		}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return p, fmt.Errorf("图片无法解析: %w", err)
	}
	w, h := img.Width, img.Height
	if w == 0 && h == 0 {
		w = float64(config.Width) / 96 * 25.4
		h = float64(config.Height) / 96 * 25.4
	}
	if w == 0 {
		w = h * float64(config.Width) / float64(config.Height)
	}
	if h == 0 {
		h = w * float64(config.Height) / float64(config.Width)
	}
	if w > e.contentWidth {
		h *= e.contentWidth / w
		w = e.contentWidth
	}
	p.image = &creator.Image{Width: w, Height: h, Data: img.Data, Format: format}
	p, err = e.prepareFigureCaption(p, img.Caption)
	if err != nil {
		return p, err
	}
	if p.height() > e.contentHeight {
		return p, fmt.Errorf("图片与图注总高超过整页版心，请减小图片尺寸")
	}
	return p, nil
}

func (e *engine) placeImage(p *preparedBlock) {
	e.ensureSpace(p.height())
	if e.hasContent() {
		e.space(p.style.SpaceBefore)
	}
	e.recordAnchor(p.block.ID)
	img := *p.image
	e.drawLeftBorder(p.block.LeftBorderColor, p.indent, e.y, img.Height)
	img.X = e.contentLeft + p.indent + (e.contentWidth-p.indent-img.Width)/2
	img.Y = e.geometry.PageHeight - e.y
	e.add(img)
	e.y -= img.Height
	e.drawFigureCaption(p)
}
