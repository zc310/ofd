// Package rastercore 提供可切换栅格后端共用的渲染上下文状态机。
//
// gg、ftgg、tinyskia 三个后端只有「画笔/路径提交/图像合成」随光栅库不同，
// 其余语义完全一致：Push/Pop 样式镜像、逻辑矩阵累计、毫米→设备像素换算、
// 描边造型缩放、canvas.Path 扫描、DrawImage 矩阵拼装、CopyStrokeToFill 等。
// 这些共享逻辑集中在本包，各后端只需实现 Hooks。
//
// Core 直接满足 render.DrawContext（并带 Raster），因此各后端 New 只需
// 用一个工厂函数构造 Hooks 即可返回 render.Backend。
package rastercore

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/render/backends/rasterstate"
)

// StrokeStyle 是换算到设备像素后的描边造型，交给 Hooks 写入光栅库。
type StrokeStyle struct {
	Width      float64 // 设备像素线宽
	Cap        canvas.Capper
	Join       canvas.Joiner
	MiterLimit float64
	Dashes     []float64 // 原始（逻辑毫米）虚线数组，由 Hooks 按线宽缩放
	DashOffset float64   // 原始（逻辑毫米）相位
}

// DeviceOp 是设备路径的一段。
type DeviceOp byte

const (
	// MoveOp/LineOp/QuadOp/CubeOp/CloseOp 对应路径段类型。
	MoveOp DeviceOp = iota
	LineOp
	QuadOp
	CubeOp
	CloseOp
)

// DeviceSeg 是一段设备像素坐标的路径。P 按段类型使用前 1~3 个点。
type DeviceSeg struct {
	Op DeviceOp
	P  [3]canvas.Point
}

// DevicePath 是复用缓冲的设备路径，避免每次绘制都分配新切片。
type DevicePath struct {
	Segs []DeviceSeg
}

// Reset 清空路径段，保留底层容量。
func (p *DevicePath) Reset() { p.Segs = p.Segs[:0] }

// MoveTo 追加一个移动到点。
func (p *DevicePath) MoveTo(x, y float64) {
	p.Segs = append(p.Segs, DeviceSeg{Op: MoveOp, P: [3]canvas.Point{{X: x, Y: y}}})
}

// LineTo 追加一个直线段。
func (p *DevicePath) LineTo(x, y float64) {
	p.Segs = append(p.Segs, DeviceSeg{Op: LineOp, P: [3]canvas.Point{{X: x, Y: y}}})
}

// QuadTo 追加一个二次贝塞尔段。
func (p *DevicePath) QuadTo(cx, cy, x, y float64) {
	p.Segs = append(p.Segs, DeviceSeg{Op: QuadOp, P: [3]canvas.Point{{X: cx, Y: cy}, {X: x, Y: y}}})
}

// CubeTo 追加一个三次贝塞尔段。
func (p *DevicePath) CubeTo(c1x, c1y, c2x, c2y, x, y float64) {
	p.Segs = append(p.Segs, DeviceSeg{
		Op: CubeOp,
		P:  [3]canvas.Point{{X: c1x, Y: c1y}, {X: c2x, Y: c2y}, {X: x, Y: y}},
	})
}

// Close 追加一个闭合段。
func (p *DevicePath) Close() {
	p.Segs = append(p.Segs, DeviceSeg{Op: CloseOp})
}

// Hooks 是共享状态机与具体光栅库之间的桥。Core 负责渲染上下文语义，Hooks
// 只做与库相关的画笔设置、路径提交与图像合成。
type Hooks interface {
	// Push/Pop 保存并恢复光栅库自身的画笔与变换。
	Push()
	Pop()

	// SetFillSolid/SetStrokeSolid 设置纯色画笔；ClearFill/ClearStroke 清除。
	SetFillSolid(c color.Color)
	SetStrokeSolid(c color.Color)
	ClearFill()
	ClearStroke()

	// SetFillRule 设置光栅库的填充规则。
	SetFillRule(rule canvas.FillRule)

	// DrawDevicePath 提交已换算到设备像素坐标的路径，并按需填充/描边。
	// m 是当次绘制的完整逻辑矩阵（渐变采样/几何换算用）；fillGrad 与
	// strokeGrad 为 nil 表示使用当前已设置的纯色画笔。Core 已保证 fill 与
	// stroke 至少一个为真，且 stroke 为真时 style.Width>0。
	DrawDevicePath(path *DevicePath, m canvas.Matrix, fill, stroke bool, fillGrad, strokeGrad canvas.Gradient, style StrokeStyle)

	// RenderImage 按绝对设备矩阵合成预渲染图像（离屏合成/mesh/文字图层）。
	RenderImage(img image.Image, m canvas.Matrix)

	// Raster 返回当前绘制的全部内容对应的 RGBA 图像。
	Raster() *image.RGBA
}

// Core 是 DrawContext 的共享实现。像素尺寸与 canvas 的 Rasterize 取整
// 规则一致（int(w*dpmm+0.5)）。
type Core struct {
	w, h float64 // 页面物理尺寸 mm
	dpmm float64 // 每毫米像素
	hpix float64 // 像素缓冲高（float 形式）

	hooks Hooks
	mx    canvas.Matrix

	device DevicePath

	fillActive   bool
	strokeActive bool
	strokeWidth  float64
	strokeCap    canvas.Capper
	strokeJoin   canvas.Joiner
	miterLimit   float64
	dashOffset   float64
	dashes       []float64
	fillGradient canvas.Gradient
	strokeGrad   canvas.Gradient
	strokePaint  canvas.Paint
	fillRule     canvas.FillRule

	stack []rasterstate.State
}

// New 计算像素尺寸并调用 factory 构造光栅库的 Hooks。factory 收到已经过
// 取整校验的 wpx/hpx（像素）与 dpmm/hpix，返回库相关的 Hooks 实现。
func New(width, height float64, resolution canvas.Resolution, factory func(wpx, hpx int, dpmm, hpix float64) Hooks) (*Core, error) {
	dpmm := resolution.DPMM()
	wpx := int(width*dpmm + 0.5)
	hpx := int(height*dpmm + 0.5)
	if wpx <= 0 || hpx <= 0 {
		return nil, fmt.Errorf("栅格尺寸无效：%g×%gmm @ %v（%d×%d 像素）", width, height, resolution, wpx, hpx)
	}
	return &Core{
		w:     width,
		h:     height,
		dpmm:  dpmm,
		hpix:  float64(hpx),
		hooks: factory(wpx, hpx, dpmm, float64(hpx)),
		mx:    canvas.Identity,
		// canvas 默认样式：填充黑色、描边空、线宽 1；MiterJoin 即
		// MiterJoiner{GapJoiner: BevelJoin, Limit: 4.0}。
		fillActive: true,
		strokeCap:  canvas.ButtCap,
		strokeJoin: canvas.MiterJoin,
		miterLimit: 4.0,
	}, nil
}

// MatrixScale 返回矩阵的不变缩放因子（行列式开方），用于线宽/虚线换算。
func MatrixScale(m canvas.Matrix) float64 {
	det := m[0][0]*m[1][1] - m[0][1]*m[1][0]
	sf := math.Sqrt(math.Abs(det))
	if !(sf > 0) || math.IsNaN(sf) {
		return 1.0
	}
	return sf
}

// Push 保存当前样式与变换。
func (c *Core) Push() {
	c.stack = append(c.stack, rasterstate.Save(
		c.mx, c.fillActive, c.strokeActive, c.strokeWidth, c.strokeCap, c.strokeJoin,
		c.miterLimit, c.dashOffset, c.dashes, c.fillGradient, c.strokeGrad, c.strokePaint, c.fillRule))
	c.hooks.Push()
}

// Pop 恢复最近一次 Push 保存的样式与变换。
func (c *Core) Pop() {
	if len(c.stack) == 0 {
		c.hooks.Pop()
		return
	}
	s := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	c.mx = s.Mx
	c.fillActive = s.FillActive
	c.strokeActive = s.StrokeActive
	c.strokeWidth = s.StrokeWidth
	c.strokeCap = s.StrokeCap
	c.strokeJoin = s.StrokeJoin
	c.miterLimit = s.MiterLimit
	c.dashOffset = s.DashOffset
	c.dashes = s.Dashes
	c.fillGradient = s.FillGradient
	c.strokeGrad = s.StrokeGrad
	c.strokePaint = s.StrokePaint
	c.SetFillRule(s.FillRule)
	c.hooks.Pop()
}

// Translate/Scale/Rotate 累积逻辑矩阵（设备换算在 DrawPath 执行）。
func (c *Core) Translate(x, y float64) { c.mx = c.mx.Translate(x, y) }
func (c *Core) Scale(sx, sy float64)   { c.mx = c.mx.Scale(sx, sy) }
func (c *Core) Rotate(deg float64)     { c.mx = c.mx.Rotate(deg) }

// CurrentMatrix 返回 canvas 表示（CoordSystem·View）的当前逻辑矩阵。
func (c *Core) CurrentMatrix() canvas.Matrix { return c.mx }

// SetFillColor 设置纯色填充。
func (c *Core) SetFillColor(col color.Color) {
	c.fillActive = true
	c.fillGradient = nil
	c.hooks.SetFillSolid(col)
}

// SetFillGradient 设置渐变填充（延迟到 DrawPath 时按矩阵采样）。
func (c *Core) SetFillGradient(g canvas.Gradient) {
	c.fillActive = true
	c.fillGradient = g
}

// SetFillPaint 设置填充画笔（颜色、渐变或清除）。
func (c *Core) SetFillPaint(paint canvas.Paint) {
	if paint.IsColor() {
		c.SetFillColor(paint.Color)
	} else if paint.IsGradient() {
		c.SetFillGradient(paint.Gradient)
	} else {
		c.ClearFill()
	}
}

// ClearFill 清除填充。
func (c *Core) ClearFill() {
	c.fillActive = false
	c.fillGradient = nil
	c.hooks.ClearFill()
}

// SetStrokeColor 设置纯色描边。
func (c *Core) SetStrokeColor(col color.Color) {
	c.strokeActive = true
	c.strokeGrad = nil
	c.strokePaint = rasterstate.SolidPaint(col)
	c.hooks.SetStrokeSolid(col)
}

// SetStrokeGradient 设置描边渐变（延迟到 DrawPath 时按矩阵采样）。
func (c *Core) SetStrokeGradient(g canvas.Gradient) {
	c.strokeActive = true
	c.strokeGrad = g
	c.strokePaint = canvas.Paint{Gradient: g}
}

// SetStrokePaint 设置描边画笔（颜色、渐变或清除）。
func (c *Core) SetStrokePaint(paint canvas.Paint) {
	if paint.IsColor() {
		c.SetStrokeColor(paint.Color)
	} else if paint.IsGradient() {
		c.SetStrokeGradient(paint.Gradient)
	} else {
		c.ClearStroke()
	}
}

// ClearStroke 清除描边。
func (c *Core) ClearStroke() {
	c.strokeActive = false
	c.strokeGrad = nil
	c.strokePaint = canvas.Paint{}
	c.hooks.ClearStroke()
}

// CopyStrokeToFill 把当前描边画笔真正复制为填充画笔（含纯色、渐变）。
func (c *Core) CopyStrokeToFill() {
	c.SetFillPaint(c.strokePaint)
}

// SetStrokeWidth 设置描边宽度（逻辑毫米）。
func (c *Core) SetStrokeWidth(w float64) { c.strokeWidth = w }

// SetDashes 设置虚线数组与相位。
func (c *Core) SetDashes(offset float64, dashes ...float64) {
	c.dashOffset = offset
	c.dashes = append([]float64(nil), dashes...)
}

// SetStrokeCapper 设置线帽。
func (c *Core) SetStrokeCapper(cap canvas.Capper) { c.strokeCap = cap }

// SetStrokeJoiner 设置连接器，并记录斜接限制。
func (c *Core) SetStrokeJoiner(join canvas.Joiner) {
	c.strokeJoin = join
	if j, ok := join.(canvas.MiterJoiner); ok {
		c.miterLimit = j.Limit
	}
}

// StrokeWidth 返回当前线宽。
func (c *Core) StrokeWidth() float64 { return c.strokeWidth }

// StrokeCapper 返回当前线帽。
func (c *Core) StrokeCapper() canvas.Capper { return c.strokeCap }

// StrokeJoiner 返回当前连接器。
func (c *Core) StrokeJoiner() canvas.Joiner { return c.strokeJoin }

// SetFillRule 设置填充规则。
func (c *Core) SetFillRule(rule canvas.FillRule) {
	c.fillRule = rule
	c.hooks.SetFillRule(rule)
}

// DrawPath 按当前样式绘制路径（x,y 为路径平移到画布的偏移）。
func (c *Core) DrawPath(x, y float64, p *canvas.Path) {
	stroke := c.strokeActive && c.strokeWidth > 0
	if p == nil || p.Empty() || math.IsNaN(x) || math.IsNaN(y) || !c.fillActive && !stroke {
		return
	}
	m := c.mx.Translate(x, y)
	c.buildDevice(p, m)
	style := StrokeStyle{
		Width:      c.strokeWidth * c.dpmm * MatrixScale(m),
		Cap:        c.strokeCap,
		Join:       c.strokeJoin,
		MiterLimit: c.miterLimit,
		Dashes:     c.dashes,
		DashOffset: c.dashOffset,
	}
	c.hooks.DrawDevicePath(&c.device, m, c.fillActive, stroke, c.fillGradient, c.strokeGrad, style)
}

// TextPath 绘制字形轮廓路径（同 DrawPath）。
func (c *Core) TextPath(p *canvas.Path, x, y float64) { c.DrawPath(x, y, p) }

// DrawImage 绘制图片：dpmm 是源图像素到画布单位（mm）的换算。
func (c *Core) DrawImage(img image.Image, x, y float64, dpmm float64) {
	if img == nil || img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		return
	}
	m := c.mx.Translate(x, y).Scale(1.0/dpmm, 1.0/dpmm)
	c.RenderImage(img, m)
}

// RenderImage 按绝对设备矩阵绘制预渲染图片。
func (c *Core) RenderImage(img image.Image, m canvas.Matrix) {
	if img == nil || img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		return
	}
	c.hooks.RenderImage(unwrapImage(img), m)
}

// decodedImage 是延迟解码图片包装器（如 canvas/image.Image）暴露原生
// image.Image 的最小接口。
type decodedImage interface {
	Image() (image.Image, error)
}

// unwrapImage 把延迟解码的图片包装器替换为已解码的原生图像。x/image/draw
// 对 *image.RGBA/*image.NRGBA/*image.YCbCr 等具体类型有直接采样快路径，而
// 包装器的 At 每次调用都要经接口返回并在解码层再取一次 color.Color，逐像素
// 采样会产生大量堆分配。包装器内部会缓存解码结果，这里调用开销很小。
func unwrapImage(img image.Image) image.Image {
	if wrapper, ok := img.(decodedImage); ok {
		if decoded, err := wrapper.Image(); err == nil && decoded != nil && !decoded.Bounds().Empty() {
			return decoded
		}
	}
	return img
}

// Raster 返回当前光栅库绘制的 RGBA 图像。
func (c *Core) Raster() *image.RGBA { return c.hooks.Raster() }

// buildDevice 把 canvas 路径（D 空间）按完整逻辑矩阵 m 换算为设备像素坐标
// device = {dpmm·X, Hpix − dpmm·Y}；椭圆弧先用 ReplaceArcs 展开为三次贝塞尔。
func (c *Core) buildDevice(p *canvas.Path, m canvas.Matrix) {
	c.device.Reset()
	dpmm, hpix := c.dpmm, c.hpix
	toDev := func(pt canvas.Point) (float64, float64) {
		lx := m[0][0]*pt.X + m[0][1]*pt.Y + m[0][2]
		ly := m[1][0]*pt.X + m[1][1]*pt.Y + m[1][2]
		return dpmm * lx, hpix - dpmm*ly
	}
	sc := p.ReplaceArcs().Scanner()
	for sc.Scan() {
		switch sc.Cmd() {
		case canvas.MoveToCmd:
			x, y := toDev(sc.End())
			c.device.MoveTo(x, y)
		case canvas.LineToCmd:
			x, y := toDev(sc.End())
			c.device.LineTo(x, y)
		case canvas.QuadToCmd:
			cx, cy := toDev(sc.CP1())
			x, y := toDev(sc.End())
			c.device.QuadTo(cx, cy, x, y)
		case canvas.CubeToCmd:
			c1x, c1y := toDev(sc.CP1())
			c2x, c2y := toDev(sc.CP2())
			x, y := toDev(sc.End())
			c.device.CubeTo(c1x, c1y, c2x, c2y, x, y)
		case canvas.CloseCmd:
			c.device.Close()
		}
	}
}
