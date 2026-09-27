package render

import (
	"image/color"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// 文字渐变的取样空间
//
// OFD 的渐变坐标定义在图元 Boundary 内（毫米，y 向下）。但后端渲染文字时，
// canvas 的 RenderPath 会用矩阵逆把每个像素映回该文字 run 的字体空间后再取渐变
// （canvas/renderers/rasterizer/rasterizer.go 的 mInv 分支）：该空间以 run 的
// 基线起点为原点、y 轴向上，且每个 span 各自独立。直接把 Boundary 空间的渐变
// 交给后端会导致：
//
//   - y 轴方向与 OFD 相反，竖直或斜向渐变取到反方向的色；
//   - 渐变按每个 span 重新开始，跨 run 不连续；
//   - TextCode 带非零 X/Y 偏移时整段渐变平移错位。
//
// 水平渐变恰好对 y 方向不敏感、且 TextCode.X 为 0 时 x 原点重合，所以轴向渐变
// 文字看起来正常，属于巧合而非正确行为。
//
// 下面两个包装器把 Boundary 空间的渐变映射回实际取样空间。

// textRunGradient 把 Boundary 空间渐变映射到某个文字 run 的排版空间。
// 取样点 (x,y) 是 run 字体空间坐标：x 自 run 起点起算、y 自基线向上。
type textRunGradient struct {
	gradient geom.Gradient
	origin   geom.Point
	hScale   float64
}

// At 实现 geom.Gradient。
func (g textRunGradient) At(x, y float64) color.RGBA {
	// 排版时按 textAdvance 已把 HScale 计入 run 原点位移，而字形本身由后端在
	// 绘制时缩放，因此这里只需对字形的横坐标乘回 HScale 即可还原 Boundary 坐标。
	return g.gradient.At(x*g.hScale+g.origin.X, g.origin.Y-y)
}

// textDeviceGradient 把 Boundary 空间渐变映射到设备空间，供把排版矩阵烘焙进
// 路径轮廓后按设备坐标取样的场合使用（drawTextPath 的非 parentCTM 分支）。
type textDeviceGradient struct {
	gradient geom.Gradient
	inverse  geom.Matrix
}

// At 实现 geom.Gradient。
func (g textDeviceGradient) At(x, y float64) color.RGBA {
	point := g.inverse.Dot(geom.Point{X: x, Y: y})
	return g.gradient.At(point.X, point.Y)
}

// textFaces 按文字 run 提供字体面。
//
// 渐变填充必须逐 run 建立字体面：字体面的画笔会被后端按该 run 的排版空间取样，
// 而 OFD 渐变坐标属于图元 Boundary 空间。纯色填充与字体轮廓几何对 run 无关，
// 复用同一个字体面即可。
type textFaces struct {
	engine FontEngine
	family FontFamily
	object models.TextObject
	// fill 是 Boundary 空间的填充色，渐变尚未按 run 变换。
	fill *CTColor
	// base 是非渐变场景复用的字体面。
	base FontFace
	// hScale 是文字的水平缩放比例。
	hScale float64
}

// newTextFaces 建立按 run 提供字体面的辅助对象。fill 为 Boundary 空间的填充色。
func newTextFaces(engine FontEngine, family FontFamily, object models.TextObject, fill *CTColor, base FontFace, hScale float64) *textFaces {
	return &textFaces{engine: engine, family: family, object: object, fill: fill, base: base, hScale: hScale}
}

// gradient 返回 Boundary 空间的渐变；非渐变填充返回 nil。
func (t *textFaces) gradient() geom.Gradient {
	if t == nil || t.fill == nil {
		return nil
	}
	return t.fill.Gradient
}

// run 返回该 run 使用的字体面。渐变填充会按 run 原点重建字体面，使渐变在
// Boundary 空间取样；其余情况复用共享字体面。
func (t *textFaces) run(runX, runY float64) FontFace {
	if t == nil {
		return nil
	}
	gradient := t.gradient()
	if gradient == nil || t.engine == nil || t.family == nil {
		return t.base
	}
	if !finiteFloat(runX) || !finiteFloat(runY) || !finiteFloat(t.hScale) {
		return t.base
	}
	hScale := t.hScale
	if hScale <= 0 {
		hScale = 1
	}
	runFill := &CTColor{
		Value:    t.fill.Value,
		HasValue: t.fill.HasValue,
		Gradient: textRunGradient{gradient: gradient, origin: geom.Point{X: runX, Y: runY}, hScale: hScale},
	}
	if face := t.engine.FaceObject(t.family, t.object, runFill); face != nil {
		return face
	}
	return t.base
}

// runPaint 返回该 run 在其排版空间中取样的填充画笔，用于走路径绘制的文字。
func (t *textFaces) runPaint(runX, runY float64, base geom.Paint) geom.Paint {
	gradient := t.gradient()
	if gradient == nil || !finiteFloat(runX) || !finiteFloat(runY) || !finiteFloat(t.hScale) {
		return base
	}
	hScale := t.hScale
	if hScale <= 0 {
		hScale = 1
	}
	return geom.GradientPaint(textRunGradient{gradient: gradient, origin: geom.Point{X: runX, Y: runY}, hScale: hScale})
}

// devicePaint 返回按设备空间取样的填充画笔。matrix 是把图元 Boundary 空间
// 映射到设备空间的排版矩阵；矩阵不可逆时退回 run 空间画笔，由调用方决定。
func (t *textFaces) devicePaint(matrix geom.Matrix, fallback geom.Paint) (geom.Paint, bool) {
	gradient := t.gradient()
	if gradient == nil {
		return fallback, true
	}
	det := matrix.Det()
	if !finiteFloat(det) || geom.Equal(det, 0.0) {
		return fallback, false
	}
	return geom.GradientPaint(textDeviceGradient{gradient: gradient, inverse: matrix.Inv()}), true
}
