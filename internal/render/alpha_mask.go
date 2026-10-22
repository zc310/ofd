package render

import (
	"image"
	"image/color"

	"github.com/zc310/ofd/internal/render/geom"
)

// AlphaMaskSurface 直接把实色路径栅格化到单通道覆盖率图像。
type AlphaMaskSurface interface {
	SetFillColor(c color.Color)
	DrawPath(x, y float64, path *geom.Path)
	RasterAlpha() *image.Alpha
}

var newAlphaMaskSurfaceFactory func(width, height float64, resolution geom.Resolution) AlphaMaskSurface

// RegisterAlphaMaskSurfaceFactory 注册单通道裁剪掩码表面的工厂。
func RegisterAlphaMaskSurfaceFactory(fn func(width, height float64, resolution geom.Resolution) AlphaMaskSurface) {
	if fn != nil {
		newAlphaMaskSurfaceFactory = fn
	}
}

func newAlphaMaskSurface(width, height float64, resolution geom.Resolution) AlphaMaskSurface {
	if newAlphaMaskSurfaceFactory == nil {
		return nil
	}
	return newAlphaMaskSurfaceFactory(width, height, resolution)
}
