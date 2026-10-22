package render

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/render/geom"
)

// paintResolution 按目标光栅分辨率及当前容器缩放计算局部栅格密度。
// 矢量输出没有固定设备DPI，保留300DPI的默认质量，显式更高DPI仍可提高。
func (p *Document) paintResolution(ctx DrawContext) geom.Resolution {
	dpi := max(meshGradientDPI, p.dpi.DPI())
	if raster, ok := ctx.(interface{ RasterResolution() geom.Resolution }); ok {
		dpi = raster.RasterResolution().DPI()
	}
	m := ctx.CurrentMatrix()
	// 线性变换的最大奇异值：非等比缩放与错切也不能低估采样密度。
	x, y := m[0][0]*m[0][0]+m[1][0]*m[1][0], m[0][1]*m[0][1]+m[1][1]*m[1][1]
	cross := m[0][0]*m[0][1] + m[1][0]*m[1][1]
	scale := math.Sqrt((x + y + math.Hypot(x-y, 2*cross)) / 2)
	if scale > 0 && finiteFloat(scale) {
		dpi *= scale
	}
	if !finiteFloat(dpi) || dpi <= 0 {
		dpi = meshGradientDPI
	}
	return geom.DPI(min(1200, max(10, dpi)))
}

// OffscreenSurface 是离屏栅格表面：绘制完成后取回 RGBA。用于网格渐变、复合
// 单元、图片掩码等需要先在独立画布上绘制再贴回的场景。
type OffscreenSurface interface {
	DrawContext
	// Raster 返回离屏内容对应的 RGBA 图像。
	Raster() *image.RGBA
}

// newOffscreenSurfaceFactory 由 canvas 后端注册；未注册时相关功能会安全跳过。
var newOffscreenSurfaceFactory func(width, height float64, resolution geom.Resolution) OffscreenSurface

// RegisterOffscreenSurfaceFactory 注册离屏表面工厂，供 canvas 后端注册或测试注入。
func RegisterOffscreenSurfaceFactory(fn func(width, height float64, resolution geom.Resolution) OffscreenSurface) {
	if fn != nil {
		newOffscreenSurfaceFactory = fn
	}
}

// newOffscreenSurface 创建离屏表面；未注册时返回 nil，调用方需判空跳过。
func newOffscreenSurface(width, height float64, resolution geom.Resolution) OffscreenSurface {
	if newOffscreenSurfaceFactory == nil {
		return nil
	}
	return newOffscreenSurfaceFactory(width, height, resolution)
}
