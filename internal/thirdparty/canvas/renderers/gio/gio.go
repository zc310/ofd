package gio

import (
	"image"
	"image/color"
	"image/draw"
	"log/slog"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
)

type Gio struct {
	ops            *op.Ops
	width, height  float64
	xScale, yScale float64
	dimensions     layout.Dimensions
	gradientScale  float32
	nativeLinear   bool
	rasterDensity  float64
}

// SetGradientScale 告知渲染器调用方在外层施加的等比缩放。Gio 当前 GPU
// 后端不会将该缩放自动应用到 LinearGradientOp 的色轴，须在记录时补偿。
// 外层平移使用整数像素；其他外层变换应合入 RenderPath 的矩阵。
func (r *Gio) SetGradientScale(scale float32) { r.gradientScale = scale }

// HasNativeGradients 用于判断外层缩放变化时是否需要重建操作记录。
func (r *Gio) HasNativeGradients() bool { return r.nativeLinear }

// SetRasterResolution 设置非原生画笔回退的页面像素密度（像素/毫米），须包含
// 内外层缩放。未设置或值无效时保留 150 DPI；单张回退图仍最多 2048×2048。
// 调用方若重放缓存操作，目标密度变化后须重新记录才能提高采样质量。
func (r *Gio) SetRasterResolution(resolution canvas.Resolution) {
	r.rasterDensity = float64(resolution)
	if !finite(r.rasterDensity) || r.rasterDensity <= 0 {
		r.rasterDensity = 0
	}
}

// New returns a Gio renderer of fixed size.
func New(gtx layout.Context, width, height float64) *Gio {
	dimensions := layout.Dimensions{Size: image.Point{int(width + 0.5), int(height + 0.5)}}
	return &Gio{
		ops:        gtx.Ops,
		width:      width,
		height:     height,
		xScale:     1.0,
		yScale:     1.0,
		dimensions: dimensions,
	}
}

// NewContain returns a Gio renderer that fills the constraints either horizontally or vertically, whichever is met first.
func NewContain(gtx layout.Context, width, height float64) *Gio {
	xScale := float64(gtx.Constraints.Max.X) / width
	yScale := float64(gtx.Constraints.Max.Y) / height
	if yScale < xScale {
		xScale = yScale
	} else {
		yScale = xScale
	}

	dimensions := layout.Dimensions{Size: image.Point{int(width*xScale + 0.5), int(height*yScale + 0.5)}}
	return &Gio{
		ops:        gtx.Ops,
		width:      width,
		height:     height,
		xScale:     xScale,
		yScale:     yScale,
		dimensions: dimensions,
	}
}

// NewStretch returns a Gio renderer that stretches the view to fit the constraints.
func NewStretch(gtx layout.Context, width, height float64) *Gio {
	xScale := float64(gtx.Constraints.Max.X) / width
	yScale := float64(gtx.Constraints.Max.Y) / height

	dimensions := layout.Dimensions{Size: image.Point{int(width*xScale + 0.5), int(height*yScale + 0.5)}}
	return &Gio{
		ops:        gtx.Ops,
		width:      width,
		height:     height,
		xScale:     xScale,
		yScale:     yScale,
		dimensions: dimensions,
	}
}

// Dimensions returns the dimensions for Gio.
func (r *Gio) Dimensions() layout.Dimensions {
	return r.dimensions
}

// Size returns the size of the canvas in millimeters.
func (r *Gio) Size() (float64, float64) {
	return r.width, r.height
}

func (r *Gio) point(p canvas.Point) f32.Point {
	return f32.Pt(float32(r.xScale*p.X), float32(r.yScale*(r.height-p.Y)))
}

// pathClip 只读扫描路径，在提交控制点时应用变换，避免为字形和填充分配临时路径。
// 圆弧保留先整体变换、再转贝塞尔的旧顺序，以免改变椭圆近似及退化情况。
func (r *Gio) pathClip(path *canvas.Path, m canvas.Matrix) clip.Op {
	for scanner := path.Scanner(); scanner.Scan(); {
		if scanner.Cmd() == canvas.ArcToCmd {
			path = path.Copy().Transform(m).ReplaceArcs()
			m = canvas.Identity
			break
		}
	}
	point := func(p canvas.Point) f32.Point { return r.point(m.Dot(p)) }

	p := clip.Path{}
	p.Begin(r.ops)
	for scanner := path.Scanner(); scanner.Scan(); {
		switch scanner.Cmd() {
		case canvas.MoveToCmd:
			p.MoveTo(point(scanner.End()))
		case canvas.LineToCmd:
			p.LineTo(point(scanner.End()))
		case canvas.QuadToCmd:
			p.QuadTo(point(scanner.CP1()), point(scanner.End()))
		case canvas.CubeToCmd:
			p.CubeTo(point(scanner.CP1()), point(scanner.CP2()), point(scanner.End()))
		case canvas.CloseCmd:
			p.Close()
		}
	}

	return (clip.Outline{Path: p.End()}).Op()
}

func (r *Gio) renderPath(path *canvas.Path, fill canvas.Paint, m canvas.Matrix) {
	defer r.pathClip(path, m).Push(r.ops).Pop()
	paint.Fill(r.ops, toNRGBA(fill.Color))
}

// RenderPath renders a path to the canvas using a style and a transformation matrix.
func (r *Gio) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	if style.HasFill() {
		if style.Fill.IsColor() {
			fill := path
			// GB/T 33190—2016 9.2：奇偶规则先规整为带孔轮廓，再交给 Gio NonZero 裁剪。
			if style.FillRule != canvas.NonZero {
				fill = fill.Copy().Settle(style.FillRule)
			}
			r.renderPath(fill, style.Fill, m)
		} else {
			fillStyle := style
			fillStyle.Stroke = canvas.Paint{}
			r.renderPaint(path, fillStyle, m)
		}
	}

	if style.HasStroke() {
		if !style.Stroke.IsColor() {
			strokeStyle := style
			strokeStyle.Fill = canvas.Paint{}
			r.renderPaint(path, strokeStyle, m)
			return
		}
		path = path.Copy()
		if style.IsDashed() {
			dashOffset, dashes := canvas.ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
			path = path.Dash(dashOffset, dashes...)
		}
		path = path.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, canvas.Tolerance)
		r.renderPath(path, style.Stroke, m)
	}
}

// renderPaint 优先使用原生线性渐变；其余画笔使用已有采样和变换语义
// 局部回退。参考 GB/T 33190—2016 8.3、8.4；不重新解释 OFD。
// 目标采样密度由调用方指定，默认 150 DPI，每个画笔最多 2048×2048 像素。
// 采样密度和预算是实现策略，不是标准对输出设备的要求（§8.3.4.1）。
func (r *Gio) renderPaint(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	if r.renderUniformRadial(path, style, m) {
		return
	}
	if r.renderLinearGradient(path, style, m) {
		return
	}
	outline := path.Copy()
	if style.HasStroke() {
		if style.IsDashed() {
			offset, dashes := canvas.ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
			outline = outline.Dash(offset, dashes...)
		}
		outline = outline.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, canvas.Tolerance)
	}
	bounds := outline.Transform(m).Bounds().And(canvas.Rect{X1: r.width, Y1: r.height})
	if bounds.Empty() || !finite(bounds.W()) || !finite(bounds.H()) {
		return
	}
	resolution := r.paintResolution(bounds)
	w, h := max(1, min(2048, int(math.Ceil(bounds.W()*resolution)))), max(1, min(2048, int(math.Ceil(bounds.H()*resolution))))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	ras := rasterizer.FromImage(img, canvas.Resolution(resolution), canvas.LinearColorSpace{})
	ras.RenderPath(path, style, canvas.Identity.Translate(-bounds.X0, -bounds.Y0).Mul(m))
	ras.Close()
	// 位图尺寸按像素向上取整，其实际覆盖范围可能超出页面右侧或上侧。
	// 裁剪在页面坐标下进行，以保留小数尺寸及外层缩放。
	pageWidth, pageHeight := r.width*r.xScale, r.height*r.yScale
	pageClip := r.polygonClip([]canvas.Point{{X: 0, Y: 0}, {X: pageWidth, Y: 0}, {X: pageWidth, Y: pageHeight}, {X: 0, Y: pageHeight}}).Push(r.ops)
	r.RenderImage(img, canvas.Identity.Translate(bounds.X0, bounds.Y0).Scale(1/resolution, 1/resolution))
	pageClip.Pop()
}

func (r *Gio) paintResolution(bounds canvas.Rect) float64 {
	density := r.rasterDensity
	if density == 0 {
		density = 150.0 / 25.4
	}
	return min(density, 2048/bounds.W(), 2048/bounds.H())
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// RenderText renders a text object to the canvas using a transformation matrix.
func (r *Gio) RenderText(text *canvas.Text, m canvas.Matrix) {
	text.RenderTo(r, m, 0.0)
}

// RenderImage renders an image to the canvas using a transformation matrix.
func (r *Gio) RenderImage(img image.Image, m canvas.Matrix) {
	if cimg, ok := img.(*cimage.Image); ok {
		var err error
		img, err = cimg.Image()
		if err != nil {
			slog.Error("Gio 解码图片失败", "error", err)
			return
		}
	}
	if img == nil || img.Bounds().Empty() {
		return
	}
	img = imageForGio(img)
	m = canvas.Identity.Scale(r.xScale, r.yScale).Mul(m)
	m = m.Translate(0.0, float64(img.Bounds().Dy()))
	trans := op.Affine(f32.NewAffine2D(
		float32(m[0][0]), -float32(m[0][1]), float32(m[0][2]),
		-float32(m[1][0]), float32(m[1][1]), float32(r.yScale*r.height-m[1][2]),
	)).Push(r.ops)
	defer trans.Pop()
	defer clip.Rect(image.Rectangle{Max: img.Bounds().Size()}).Push(r.ops).Pop()
	paint.NewImageOp(img).Add(r.ops)
	paint.PaintOp{}.Add(r.ops)
}

// 查表按需初始化，固定占用 64 KiB；它只替换逐通道的计算，不改变舍入语义。
var linearPremulTable = sync.OnceValue(func() *[256][256]uint8 { //nolint:gochecknoglobals // 固定大小的只读查表，在各次图片转换之间复用。
	table := new([256][256]uint8)
	for alpha := range 256 {
		for value := range 256 {
			v := float64(value) / 255
			if v <= 0.04045 {
				v /= 12.92
			} else {
				v = math.Pow((v+0.055)/1.055, 2.4)
			}
			v *= float64(alpha) / 255
			if v <= 0.0031308 {
				v *= 12.92
			} else {
				v = 1.055*math.Pow(v, 1/2.4) - 0.055
			}
			table[alpha][value] = uint8(math.Round(v * 255))
		}
	}
	return table
})

// imageForGio 将标准 Go 图像的 sRGB 预乘转换为 GPU sRGB 纹理所需的
// 线性预乘后编码值。否则半透明红色的 R=128 会在纹理解码时再次变暗，
// 与 Gio 原生的同色 ColorOp 不一致。新图只归 Gio 持有，不修改共享资源。
func imageForGio(src image.Image) image.Image {
	switch img := src.(type) {
	case *image.RGBA:
		return image8BitForGio(img, img.Pix, img.Stride, false)
	case *image.NRGBA:
		return image8BitForGio(img, img.Pix, img.Stride, true)
	}
	// JPEG、Gray 等已知不透明图像无需逐像素查询透明度；GPU 上传时仍由
	// paint.NewImageOp 完成必要的格式转换，不缓存可由调用方修改的图像。
	if opaque, ok := src.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		return imageWithZeroOrigin(src)
	}
	bounds := src.Bounds()
	var dst *image.RGBA
	var table *[256][256]uint8
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := src.At(x, y)
			_, _, _, alpha := pixel.RGBA()
			if alpha == 0 || alpha == 0xffff {
				continue
			}
			if dst == nil {
				dst = image.NewRGBA(image.Rectangle{Max: bounds.Size()})
				draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
				table = linearPremulTable()
			}
			c, _ := color.NRGBAModel.Convert(pixel).(color.NRGBA)
			// 存储值仅用于 GPU 上传，不再作为标准 Go 预乘颜色参加图像计算。
			dst.SetRGBA(x-bounds.Min.X, y-bounds.Min.Y, color.RGBA{R: table[c.A][c.R], G: table[c.A][c.G], B: table[c.A][c.B], A: c.A})
		}
	}
	if dst != nil {
		return dst
	}
	return imageWithZeroOrigin(src)
}

// RenderClippedImage 保留源图片局部、Y 向上的裁剪路径，不先生成整张掩模图片。
func (r *Gio) RenderClippedImage(img image.Image, m canvas.Matrix, path *canvas.Path) {
	if path == nil || path.Empty() {
		return
	}
	defer r.pathClip(path, m).Push(r.ops).Pop()
	r.RenderImage(img, m)
}

// RenderClippedText 与 canvas 的文字局部裁剪坐标保持一致。
func (r *Gio) RenderClippedText(text *canvas.Text, m canvas.Matrix, path *canvas.Path) {
	if path == nil || path.Empty() {
		return
	}
	defer r.pathClip(path, m).Push(r.ops).Pop()
	r.RenderText(text, m)
}

// 画笔和色标均使用 RGBA，保留具体类型以免每次提交颜色都发生接口装箱。
func toNRGBA(col color.RGBA) color.NRGBA {
	r, g, b, a := col.RGBA()
	if a == 0 {
		return color.NRGBA{}
	}
	r = (r * 0xffff) / a
	g = (g * 0xffff) / a
	b = (b * 0xffff) / a
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
