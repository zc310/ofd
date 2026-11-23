package canvas

import (
	"image"

	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"

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
	ctx := tcanvas.NewContext(c)
	ctx.SetCoordSystem(tcanvas.CartesianIV)
	b := &canvasRasterBackend{c: c, resolution: resolution}
	b.ctx = ctx
	return b
}

// Raster 按创建时指定的分辨率光栅化页面画布。
func (b *canvasRasterBackend) Raster() *image.RGBA {
	return rasterize(b.c, tcanvas.Resolution(b.resolution), tcanvas.DefaultColorSpace, nil)
}

// CanvasScene 返回后端累积的 canvas 页面记录。GPU 等外部渲染器可直接重放
// 同一记录，无需重新执行 OFD 绘制；调用方只能在本次绘制完成后读取，
// 且不得修改记录内容。
func (b *canvasRasterBackend) CanvasScene() *tcanvas.Canvas { return b.c }

func (b *canvasRasterBackend) RasterResolution() geom.Resolution { return b.resolution }

var _ drawing.Backend = (*canvasRasterBackend)(nil)
