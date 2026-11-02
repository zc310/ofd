package pdf

import (
	"fmt"

	"github.com/zc310/ofd/internal/thirdparty/canvas" //nolint:depguard // This renderer consumes canvas text and paint types.
)

// RenderTextWithPaint keeps glyph placement and clipping unchanged while using
// an independent paint matrix. Opaque native gradients can then share a Pattern
// across independently positioned runs. Unsupported text requests normal replay.
func (r *PDF) RenderTextWithPaint(
	text *canvas.Text,
	matrix canvas.Matrix,
	clip *canvas.Path,
	paint canvas.Paint,
	paintMatrix canvas.Matrix,
) bool {
	if text == nil || text.Empty() || (clip != nil && clip.Empty()) {
		return true
	}

	if !sharedTextPaintSupported(text, paint) {
		return false
	}

	draw := func() { r.renderText(text, matrix, &paint, paintMatrix) }
	if clip == nil {
		draw()
	} else {
		r.RenderClippedTextWith(text, matrix, clip, draw)
	}

	return true
}

func (r *PDF) renderText(text *canvas.Text, matrix canvas.Matrix, paint *canvas.Paint, paintMatrix canvas.Matrix) {
	const fillAndStrokeMode, strokeWidthFactor = 2, 2

	text.RenderDecorationsTo(r, matrix, 0.0)

	text.WalkSpans(func(offsetX, offsetY float64, span canvas.TextSpan) {
		if span.IsText() {
			fill := span.Face.Fill
			if paint != nil {
				fill = *paint
			}

			r.w.StartTextObject()
			r.w.SetFill(fill, paintMatrix)
			r.w.SetFont(span.Face.Font, span.Face.Size, span.Direction)
			r.w.SetTextPosition(matrix.Translate(offsetX, offsetY).Shear(span.Face.FauxItalic, 0.0))

			if 0.0 < span.Face.FauxBold {
				r.w.SetTextRenderMode(fillAndStrokeMode)
				r.w.SetStroke(fill, paintMatrix)
				_, _ = fmt.Fprintf(r.w, " %v w", dec(span.Face.FauxBold*strokeWidthFactor))
			} else {
				r.w.SetTextRenderMode(0)
			}

			r.w.WriteText(text.WritingMode, span.Glyphs)
			r.w.EndTextObject()
		} else {
			for _, obj := range span.Objects {
				obj.RenderViewTo(r, matrix.Mul(obj.View(offsetX, offsetY, span.Face)))
			}
		}
	})
}

func opaqueTextGradient(paint canvas.Paint) bool {
	const opaqueAlpha = 255

	var stops canvas.Grad

	switch gradient := paint.Gradient.(type) {
	case *canvas.LinearGradient:
		stops = gradient.Grad
	case *canvas.RadialGradient:
		stops = gradient.Grad
	default:
		return false
	}

	for _, stop := range stops {
		if stop.Color.A != opaqueAlpha {
			return false
		}
	}

	return true
}

func sharedTextPaintSupported(text *canvas.Text, paint canvas.Paint) bool {
	if !opaqueTextGradient(paint) {
		return false
	}

	valid := true

	text.WalkSpans(func(_, _ float64, span canvas.TextSpan) {
		if !span.IsText() || span.Face.FauxBold != 0 || !span.Face.Fill.Has() {
			valid = false

			return
		}

		font := span.Face.Font
		if font.Head.XMin == font.Head.XMax && font.Head.YMin == font.Head.YMax {
			valid = false
		}
	})

	return valid
}
