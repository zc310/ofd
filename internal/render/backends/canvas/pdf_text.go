package canvas

import (
	"math"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/pdf"
)

// PDF 写入器不支持渐变色标的 Alpha。仅将透明渐变文字的可见部分
// 绘制为带透明通道的局部图片，并保留不可见文字供复制、搜索和提取。
// 写入器的合成粗体描边也未按字号缩放；改用与光栅输出一致的矢量轮廓。
// 其他文字仍交给原生写入器，SVG 不经过此适配层。
type pdfTextRenderer struct {
	*pdf.PDF

	width, height float64
}

func (r pdfTextRenderer) RenderClippedText(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path) {
	r.RenderClippedTextWith(text, matrix, clip, func() { r.RenderText(text, matrix) })
}

func (r pdfTextRenderer) RenderCanvas(scene *canvas.Canvas, matrix canvas.Matrix) {
	r.PDF.RenderCanvasWith(scene, matrix, func() {
		scene.RenderTo(pdfTextRenderer{PDF: r.PDF, width: scene.W, height: scene.H})
	})
}

func (r pdfTextRenderer) RenderText(text *canvas.Text, matrix canvas.Matrix) {
	translucent, fauxBold := false, false
	text.WalkSpans(func(_, _ float64, span canvas.TextSpan) {
		if !span.IsText() {
			return
		}
		fauxBold = fauxBold || span.Face.FauxBold != 0
		var stops canvas.Grad
		switch gradient := span.Face.Fill.Gradient.(type) {
		case *canvas.LinearGradient:
			stops = gradient.Grad
		case *canvas.RadialGradient:
			stops = gradient.Grad
		}
		for _, stop := range stops {
			translucent = translucent || stop.Color.A != 255
		}
	})
	if !translucent && !fauxBold {
		r.PDF.RenderText(text, matrix)
		// 裁剪文字也会附带不可见语义层，避免其Alpha=0污染后续渐变。
		visible := false
		text.WalkSpans(func(_, _ float64, span canvas.TextSpan) {
			visible = visible || span.IsText() && span.Face.Fill.Has()
		})
		if !visible {
			r.restoreOpacity()
		}
		return
	}

	if !translucent {
		// 分辨率为 0，保持矢量文字的精确定位，不吸附到特定像素网格。
		text.RenderTo(r.PDF, matrix, 0)
	} else {
		bounds := text.OutlineBounds().Transform(matrix).Expand(0.1)
		// PDF 页面之外不显示；裁到页面范围，并限制局部栅格化的内存与单边尺寸。
		bounds.X0, bounds.Y0 = max(0, bounds.X0), max(0, bounds.Y0)
		bounds.X1, bounds.Y1 = min(r.width, bounds.X1), min(r.height, bounds.Y1)
		width, height := bounds.W(), bounds.H()
		if width > 0 && height > 0 && !math.IsInf(width, 0) && !math.IsInf(height, 0) {
			dpmm := min(300.0/25.4, math.Sqrt(4_000_000/width/height), 32768/width, 32768/height)
			local := canvas.New(width, height)
			local.RenderText(text, canvas.Identity.Translate(-bounds.X0, -bounds.Y0).Mul(matrix))
			img := rasterize(local, canvas.DPMM(dpmm), canvas.DefaultColorSpace)
			if img.Bounds().Dx() > 0 && img.Bounds().Dy() > 0 {
				r.PDF.RenderImage(img, canvas.Identity.Translate(bounds.X0, bounds.Y0).
					Scale(width/float64(img.Bounds().Dx()), height/float64(img.Bounds().Dy())))
			}
		}
	}
	text.WalkSpans(func(x, y float64, span canvas.TextSpan) {
		if !span.IsText() {
			return
		}
		face := *span.Face
		face.Fill, face.Deco, face.FauxBold = canvas.Paint{}, nil, 0
		line := canvas.NewTextLine(&face, span.Text, canvas.Left)
		line.WritingMode = text.WritingMode
		line.TextOrientation = text.TextOrientation
		// 复用原来整形得到的字形与方向，不改写共享文字对象。
		line.WalkLines(func(_ float64, spans []canvas.TextSpan) {
			if len(spans) == 1 {
				textSpan := span
				textSpan.Face, textSpan.X = &face, 0
				spans[0] = textSpan
			}
		})
		r.PDF.RenderText(line, matrix.Translate(x, y))
	})
	// 不可见文字把写入器的共享 Alpha 设为0，而后续渐变不会自行恢复它。
	// 写出没有几何内容的填充以恢复不透明状态，不留下可见图元。
	r.restoreOpacity()
}

func (r pdfTextRenderer) restoreOpacity() {
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: canvas.Black}
	style.Stroke = canvas.Paint{}
	r.PDF.RenderPath(&canvas.Path{}, style, canvas.Identity)
}
