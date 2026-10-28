package render

import (
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// AlphaMaskSurface 直接把实色路径栅格化到单通道覆盖率图像。
type AlphaMaskSurface = drawing.AlphaMaskSurface

// RegisterAlphaMaskSurfaceFactory 注册单通道裁剪掩码表面的工厂。
func RegisterAlphaMaskSurfaceFactory(fn func(width, height float64, resolution geom.Resolution) AlphaMaskSurface) {
	if fn != nil {
		drawing.RegisterAlphaMaskSurfaceFactory(fn)
	}
}

func newAlphaMaskSurface(width, height float64, resolution geom.Resolution) AlphaMaskSurface {
	return drawing.NewAlphaMaskSurface(width, height, resolution)
}
