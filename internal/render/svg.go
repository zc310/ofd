package render

import "github.com/zc310/ofd/internal/render/drawing"

// SVGScene 是与后端共享的 SVG 场景契约。
type SVGScene = drawing.SVGScene

// RegisterSVGSceneFactory 注册 SVG 场景解析器，供 canvas 后端注册或测试注入。
func RegisterSVGSceneFactory(fn func(data []byte) (SVGScene, error)) {
	drawing.RegisterSVGSceneFactory(fn)
}

// parseSVGScene 解析 SVG 数据为场景；未注册解析器时返回错误。
func parseSVGScene(data []byte) (SVGScene, error) {
	return drawing.ParseSVGScene(data)
}
