package render

import (
	"errors"

	"github.com/zc310/ofd/internal/render/drawing"
)

// SVGScene 定义在 drawing 包，render 通过别名保持既有调用点可用。
type SVGScene = drawing.SVGScene

// svgSceneFactory 是当前 SVG 解析器；默认未注册，需空白导入 canvas 后端包
// 或调用 RegisterSVGSceneFactory 注入。
var svgSceneFactory func(data []byte) (SVGScene, error)

// RegisterSVGSceneFactory 注册 SVG 场景解析器，供 canvas 后端注册或测试注入。
func RegisterSVGSceneFactory(fn func(data []byte) (SVGScene, error)) {
	svgSceneFactory = fn
}

// parseSVGScene 解析 SVG 数据为场景；未注册解析器时返回错误。
func parseSVGScene(data []byte) (SVGScene, error) {
	if svgSceneFactory == nil {
		return nil, errors.New("未注册 SVG 场景解析器（请空白导入 internal/render/backends/canvas 或注册自定义工厂）")
	}
	return svgSceneFactory(data)
}
