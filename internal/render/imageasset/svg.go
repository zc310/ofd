package imageasset

import (
	"errors"
	"image"
	"sync"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// SVGScene 是与绘制库无关的已解析 SVG 场景。
type SVGScene = drawing.SVGScene

// SVGParser 将 SVG 数据解析为可栅格化或矢量嵌入的场景。
type SVGParser = drawing.SVGParser

var errEmptySVG = errors.New("SVG 解析器未返回场景")

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
	if err != nil {
		return scene, err
	}
	if scene == nil {
		return nil, errEmptySVG
	}
	return &synchronizedSVGScene{SVGScene: scene}, nil
}
