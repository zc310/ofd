package render

import (
	"image"

	"github.com/zc310/ofd/internal/render/geom"
)

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
