package imageasset

import (
	"errors"
	"image"
	"sync"

	"github.com/zc310/ofd/internal/render/geom"
)

// SVGScene 是与绘制库无关的已解析 SVG 场景。
type SVGScene interface {
	// Width/Height 返回 SVG 的原始画布尺寸（用户单位）。
	Width() float64
	Height() float64
	// Rasterize 按分辨率栅格化为标准图像。
	Rasterize(resolution geom.Resolution) image.Image
	// RenderVector 尝试以矢量方式嵌入当前绘制上下文，返回是否完成。
	RenderVector(ctx any, matrix geom.Matrix) bool
}

// SVGParser 将 SVG 数据解析为可栅格化或矢量嵌入的场景。
type SVGParser func([]byte) (SVGScene, error)

type synchronizedSVGScene struct {
	SVGScene

	mu sync.Mutex
}

func (s *synchronizedSVGScene) Rasterize(resolution geom.Resolution) image.Image {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SVGScene.Rasterize(resolution)
}

func (s *synchronizedSVGScene) RenderVector(ctx any, matrix geom.Matrix) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SVGScene.RenderVector(ctx, matrix)
}

// ParseSVG 解析场景，并将其栅格化和矢量嵌入操作串行化，保护共享字体和渐变状态。
func ParseSVG(data []byte, parse SVGParser) (SVGScene, error) {
	if parse == nil {
		return nil, errors.New("未注册 SVG 场景解析器（请空白导入 internal/render/backends/canvas 或注册自定义工厂）")
	}
	scene, err := parse(data)
	if err != nil || scene == nil {
		return scene, err
	}
	return &synchronizedSVGScene{SVGScene: scene}, nil
}
