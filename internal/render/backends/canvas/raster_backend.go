package canvas

import (
	"image"

	tcanvas "github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// canvasRasterBackend 是可栅格化的 canvas 后端：绘制进画布后按给定分辨率
// 光栅化为 RGBA。逐像素输出与 canvas 矢量表面 Rasterize 一致，背景填充、
// 复合图元等离屏栅格化逻辑共用同一实现。
type canvasRasterBackend struct {
	canvasBackend
	c          *tcanvas.Canvas
	resolution geom.Resolution
}

func newCanvasRasterBackend(width, height float64, resolution geom.Resolution) *canvasRasterBackend {
	c := tcanvas.New(width, height)
	return &canvasRasterBackend{
		canvasBackend: canvasBackend{ctx: tcanvas.NewContext(c)},
		c:             c,
		resolution:    resolution,
	}
}

// Raster 按创建时指定的分辨率光栅化页面画布。
func (b *canvasRasterBackend) Raster() *image.RGBA {
	return rasterize(b.c, tcanvas.Resolution(b.resolution), tcanvas.DefaultColorSpace)
}

var _ drawing.Backend = (*canvasRasterBackend)(nil)
