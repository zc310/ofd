package flow

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // 注册 GIF 解码器
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器
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
	switch img.Align {
	case "", "left", "center", "right":
	default:
		return p, fmt.Errorf("%w: image.align 必须为 left、center 或 right", errContent)
	}
	switch img.Fit {
	case "", "contain":
	case "stretch":
		if img.Width == 0 || img.Height == 0 {
			return p, fmt.Errorf("%w: image.fit 为 stretch 时需要同时指定 width 和 height", errContent)
		}
	default:
		return p, fmt.Errorf("%w: image.fit 必须为 contain 或 stretch", errContent)
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
	if img.Fit == "contain" {
		scale := math.Min(w/float64(config.Width), h/float64(config.Height))
		w, h = float64(config.Width)*scale, float64(config.Height)*scale
	}
	if w <= 0 || h <= 0 || math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return p, fmt.Errorf("%w: 推算的图片尺寸必须为有限正数", errContent)
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
	img.X = e.contentLeft + p.indent
	slack := e.contentWidth - p.indent - img.Width
	switch p.block.Image.Align {
	case "left":
	case "right":
		img.X += slack
	default:
		img.X += slack / 2
	}
	img.Y = e.geometry.PageHeight - e.y
	e.add(img)
	e.y -= img.Height
	e.drawFigureCaption(p)
}
