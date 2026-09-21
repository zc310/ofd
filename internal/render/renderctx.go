package render

import (
	"image"
	"image/color"

	"github.com/tdewolff/canvas"
)

// DrawContext 是 OFD 页面绘制所依赖的最小操作面，用于把绘制后端从
// tdewolff/canvas 换成 gogpu/gg（或其他栅格后备）而不改动本包其余代码。
//
// 设计约定：
//   - 几何（*canvas.Path）、矩阵（canvas.Matrix）、渐变（canvas.Gradient）
//     保持 canvas 类型：它们是纯数学的数据结构/采样器，后端各自消费。
//     canvas.Path 段可用 path.Cmd()/Pos() 枚举；canvas.Gradient 是 At(x,y)
//     采样接口，gg 后端可把它包成 gg.Brush（逐点求色）栅格化。
//   - 文字不做布局：调用方先用 FontFace.ToPath 拍平成字形轮廓路径，
//     再经 TextPath 提交。这去掉了 canvas 文字引擎与 gg 之间的鸿沟。
//   - 描边不做几何展开：DrawPath 带描边样式时由后端自己决定（canvas
//     后端走 Stroke() 轮廓填充以保持输出一致，gg 后端走原生 Stroke）。
//
// 迁移对照（现有调用 → 本接口方法）：
//
//	ctx.Push()/ctx.Pop()               → Push/Pop
//	ctx.Translate/Scale/Rotate         → Translate/Scale/Rotate
//	ctx.CoordSystemView().Mul(ctx.View()) → CurrentMatrix（离屏合成定位用）
//	ctx.SetFillColor/SetStrokeColor    → SetFillColor/SetStrokeColor
//	ctx.SetFill(paint)/SetStroke(paint)→ SetFillPaint/SetStrokePaint
//	ctx.SetFill(nil)/SetStroke(nil)    → ClearFill/ClearStroke
//	ctx.SetFill(ctx.Style.Stroke)      → CopyStrokeToFill
//	ctx.SetFillGradient/SetStrokeGradient → SetFillGradient/SetStrokeGradient
//	ctx.SetStrokeWidth/SetDashes/SetStrokeCapper/SetStrokeJoiner → 同名
//	ctx.Style.StrokeWidth/Capper/Joiner → StrokeWidth/StrokeCapper/StrokeJoiner（几何描边展开用）
//	ctx.SetFillRule(canvas.EvenOdd)    → SetFillRule(canvas.EvenOdd)
//	ctx.DrawPath(x, y, p)              → DrawPath(x, y, p)
//	ctx.DrawText(0,0, NewTextLine(...))→ textDrawer（canvas 后端原样转发；
//	                                    其它后端走 face.ToPath 拍平后 TextPath）
//	ctx.DrawImage(x,y,img,res)         → DrawImage(x, y, img, dpmm)
//	ctx.RenderImage(img, m)            → RenderImage(img, m)
//	svg.RenderViewTo(ctx.Renderer, m)  → svgSceneRenderer（canvas 后端直接拼接
//	                                    场景；其它后端先栅格化再 RenderImage）
//
// 离屏画布（pattern/seal/mesh 用 canvas.New+NewContext 直接创建，栅格化后
// 经 RenderImage 贴回）不进入本接口：它保持 canvas 实现，两个后端共用。
type DrawContext interface {
	// Push/Pop 保存并恢复当前样式与变换。
	Push()
	Pop()

	// Translate/Scale/Rotate 修改当前变换（文档坐标 → 设备坐标）。
	Translate(x, y float64)
	Scale(sx, sy float64)
	Rotate(deg float64)

	// CurrentMatrix 返回当前完整矩阵（canvas 的 CoordSystemView·View），
	// 用于把预渲染的离屏图按正确位置贴回（替代 ctx.RenderImage 前的
	// ctx.CoordSystemView().Mul(ctx.View()).Mul(m) 拼装）。
	CurrentMatrix() canvas.Matrix

	// SetFillColor 设置纯色填充。SetFillGradient 设置渐变填充。
	SetFillColor(c color.Color)
	SetFillGradient(g canvas.Gradient)

	// SetFillPaint 设置填充画笔（颜色或渐变），等价于 canvas 的 SetFill(paint)。
	SetFillPaint(paint canvas.Paint)

	// ClearFill 清除填充（等价于 canvas 的 SetFill(nil)）。
	ClearFill()

	// SetStrokeColor 设置纯色描边。SetStrokeGradient 设置描边渐变。
	SetStrokeColor(c color.Color)
	SetStrokeGradient(g canvas.Gradient)

	// SetStrokePaint 设置描边画笔（颜色或渐变），等价于 canvas 的 SetStroke(paint)。
	SetStrokePaint(paint canvas.Paint)

	// ClearStroke 清除描边（等价于 canvas 的 SetStroke(nil)）。
	ClearStroke()

	// CopyStrokeToFill 把当前描边样式复制为填充样式
	//（等价于 canvas 的 SetFill(ctx.Style.Stroke)）。
	CopyStrokeToFill()

	// SetStrokeWidth 设置描边宽度。SetDashes 设置虚线数组。
	SetStrokeWidth(w float64)
	SetDashes(offset float64, dashes ...float64)
	SetStrokeCapper(cap canvas.Capper)
	SetStrokeJoiner(join canvas.Joiner)

	// StrokeWidth/StrokeCapper/StrokeJoiner 读回当前描边样式，
	// 用于需要把描边几何展开为填充轮廓的路径（等价于 ctx.Style.StrokeWidth 等）。
	StrokeWidth() float64
	StrokeCapper() canvas.Capper
	StrokeJoiner() canvas.Joiner

	// SetFillRule 设置填充规则（canvas.NonZero / canvas.EvenOdd）。
	SetFillRule(rule canvas.FillRule)

	// DrawPath 按当前样式绘制路径（x,y 为路径的平移到画布的偏移）。
	DrawPath(x, y float64, p *canvas.Path)

	// TextPath 绘制字形轮廓路径（字形轮廓的 baseline 位于 (0,0)，
	// 笔画向 y 负方向延伸；后端负责平移到 (x,y)）。
	TextPath(p *canvas.Path, x, y float64)

	// DrawImage 绘制图片：dpmm 是源图像素到画布单位（mm）的换算，
	// 即 canvas 的 Resolution.DPMM()。
	DrawImage(img image.Image, x, y float64, dpmm float64)

	// RenderImage 按绝对设备矩阵绘制预渲染图片（离屏合成/mesh/文字图层）。
	RenderImage(img image.Image, m canvas.Matrix)
}

// textDrawer 是支持原生文字绘制的后端的可选项接口（对应 canvas 上下文的
// DrawText）。canvas 后端实现此接口并原样转发，保留 canvas 文字整形/度量；
// gg 后端把 FontFace 拍平为字形轮廓路径后经 SetFillPaint+TextPath 重建。
// 传 *canvas.Text 而非 face+value，使调用方可以缓存整形成果（相同文本
// 只整形一次）。文档绘制代码通过类型断言使用：
//
//	if td, ok := ctx.(textDrawer); ok {
//		td.DrawTextLine(line)
//	} else {
//		p.drawTextPath(...)   // 拍平路径的通用回退
//	}
type textDrawer interface {
	// DrawTextLine 在 (0,0) 处绘制单行文本（glyph baseline 位于左下角，向 y
	// 正方向延伸），等价于 canvas 的 ctx.DrawText(0, 0, line)。
	DrawTextLine(line *canvas.Text)
}

// svgSceneRenderer 是支持矢量嵌入 SVG 画布的后端可选项接口（对应 canvas
// 的 svg.RenderViewTo(ctx.Renderer, m)）。canvas 后端把场景直接拼接到底层
// 渲染器以保持 PDF/SVG 矢量输出；其它后端可先栅格化再经 RenderImage 贴回。
// 调用方的互斥锁约束（p.svgMu）与 CurrentMatrix 拼装由文档代码负责。
type svgSceneRenderer interface {
	// RenderScene 将 svg 画布以绝对设备矩阵 m 嵌入当前画布。
	RenderScene(svg *canvas.Canvas, m canvas.Matrix)
}
