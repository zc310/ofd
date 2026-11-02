package render

import (
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/imageasset"
)

// SVGScene 是与后端共享的 SVG 场景契约。
type SVGScene = drawing.SVGScene

// parseSVGScene 解析 SVG 数据为场景；未注册解析器时返回错误。
func parseSVGScene(data []byte) (SVGScene, error) {
	return imageasset.ParseSVG(data, drawing.SVGSceneFactory())
}
