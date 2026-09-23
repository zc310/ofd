package canvas

import (
	"bytes"
	"image"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// canvasSVGScene 是 drawing.SVGScene 的 canvas 实现。
type canvasSVGScene struct {
	c *canvas.Canvas
}

func newCanvasSVGScene(data []byte) (drawing.SVGScene, error) {
	c, err := canvas.ParseSVG(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return canvasSVGScene{c: c}, nil
}

func (s canvasSVGScene) Width() float64  { return s.c.W }
func (s canvasSVGScene) Height() float64 { return s.c.H }

func (s canvasSVGScene) Rasterize(resolution geom.Resolution) image.Image {
	return rasterizer.Draw(s.c, canvas.Resolution(resolution), canvas.DefaultColorSpace)
}

func (s canvasSVGScene) RenderVector(ctx any, m geom.Matrix) bool {
	sr, ok := ctx.(svgSceneRenderer)
	if !ok {
		return false
	}
	sr.RenderScene(s.c, m)
	return true
}

// svgSceneRenderer 是 canvas 后端支持矢量嵌入 SVG 的可选接口（对应 canvas 的
// RenderViewTo）。canvasBackend 实现它。
type svgSceneRenderer interface {
	RenderScene(svg *canvas.Canvas, m geom.Matrix)
}
