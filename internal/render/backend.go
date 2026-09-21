package render

import (
	"errors"
	"image"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/parser"
)

// 后端标识，供 NewBackend / Document.RasterizePage 使用。
const (
	// BackendCanvas 使用 tdewolff/canvas 光栅器（本包默认路径，输出与
	// Page()+Rasterize 一致，无需额外导入）。
	BackendCanvas = "canvas"
	// BackendGG 使用 gogpu/gg 光栅后端。需要空白导入
	// github.com/zc310/ofd/internal/render/backends/gg 才能注册使用。
	BackendGG = "gg"
	// BackendFTGG 使用 FloatTech/gg 光栅后端（测速对照，不做逐像素 parity）。
	// 需要空白导入 github.com/zc310/ofd/internal/render/backends/ftgg。
	BackendFTGG = "ftgg"
	// BackendTinySkia 使用 tinyskia 光栅后端（tiny-skia 纯 Go 移植，测速
	// 对照）。需要空白导入 github.com/zc310/ofd/internal/render/backends/tinyskia。
	BackendTinySkia = "tinyskia"
)

// Backend 是可切换的页面栅格化后端：以 DrawContext 接口接收 OFD 页面
// 绘制指令，绘制结束后把结果取回为标准 RGBA 图像。
//
// 页面通过 Document.RasterizePage 指定后端绘制（注册表见 NewBackend）；
// 未注册的后端创建会返回错误。分辨率与页面物理尺寸在创建后端时确定，
// Raster 返回的图片尺寸与页面大小及分辨率严格对应（同 canvas 的
// Rasterize 取整规则）。
type Backend interface {
	DrawContext
	// Raster 返回当前后端绘制的全部内容对应的 RGBA 图像。
	Raster() *image.RGBA
}

// canvasRasterBackend 是可栅格化的 canvas 后端：绘制进画布后按
// NewBackend 指定的分辨率光栅化为 RGBA。逐像素输出与 Page()+Rasterize
// 一致，背景填充、复合图元等离屏栅格化逻辑共用同一实现。
type canvasRasterBackend struct {
	canvasBackend
	c          *canvas.Canvas
	resolution canvas.Resolution
}

func newCanvasRasterBackend(width, height float64, resolution canvas.Resolution) *canvasRasterBackend {
	c := canvas.New(width, height)
	return &canvasRasterBackend{
		canvasBackend: canvasBackend{ctx: canvas.NewContext(c)},
		c:             c,
		resolution:    resolution,
	}
}

// Raster 按创建时指定的分辨率光栅化页面画布。
func (b *canvasRasterBackend) Raster() *image.RGBA {
	return Rasterize(b.c, b.resolution, canvas.DefaultColorSpace)
}

// RasterizePage 把指定页面栅格化为 RGBA 图像。backendName 选用渲染后端
// （BackendCanvas 始终可用；BackendGG/BackendFTGG/BackendTinySkia 等需先
// 空白导入对应插件包完成注册）；resolution 决定输出图片的像素密度。
//
// 这是 Page()/Draw() 之外的第三种按页取图方式：前两者都绑定 tdewolff/
// canvas，RasterizePage 允许在 canvas 光栅器与其它注册后端之间切换，便于
// 各后端输出的逐像素对照与速度对比。
func (p *Document) RasterizePage(page *parser.Page, backendName string, resolution canvas.Resolution) (*image.RGBA, error) {
	if page == nil {
		return nil, errors.New("页面为空")
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	var budget renderBudget
	budget.reset()
	content := lease.Content()
	if content == nil {
		return nil, errors.New("页面内容为空")
	}
	box := content.Area.PhysicalBox
	b, err := NewBackend(backendName, box.Width, box.Height, resolution)
	if err != nil {
		return nil, err
	}
	p.drawPage(b, page, content, &budget)
	return b.Raster(), nil
}
