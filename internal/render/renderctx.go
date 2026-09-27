package render

import (
	"github.com/zc310/ofd/internal/render/drawing"
)

// DrawContext 是 drawing.DrawContext 的别名，保持本包既有调用点可用。契约
// 定义在 drawing 包，使后端插件只依赖 drawing/geom，不依赖 render/canvas。
type DrawContext = drawing.DrawContext

// textDrawer 是支持原生文字绘制的后端的可选项接口。canvas 后端实现此接口
// 并原样转发，保留 canvas 文字整形/度量；其它后端不实现，走字形轮廓路径
// 回退。run 由字体引擎的 FontFace.ShapedRun 提供，对核心不透明。
type textDrawer interface {
	// DrawTextLine 在 (0,0) 处绘制单行文本（glyph baseline 位于左下角，向 y
	// 正方向延伸）。canvas 后端把它断言为 canvas 文本并原生绘制。
	DrawTextLine(run TextRun)
}

// shadingTextBackend 由能把原生 Linear/RadialGradient 序列化为文字着色图案的
// 矢量后端实现（canvas 的 PDF/SVG 输出）。实现后，可原生表达的渐变文字保留
// 真实文字 + 原生 Shading，而不是栅格化成图片，从而在 PDF 中仍可复制。
type shadingTextBackend interface {
	ShadingText() bool
}
