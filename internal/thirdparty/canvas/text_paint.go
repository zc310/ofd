package canvas

// TextPaintRenderer optionally draws text with a uniform paint whose coordinate
// system is independent of glyph positioning. The span paints must describe the
// same visible field in their original coordinate systems, so renderers that do
// not support this extension can replay the ordinary text without approximation.
// clip is text-local, or nil for no clipping. False requests ordinary replay.
type TextPaintRenderer interface {
	RenderTextWithPaint(text *Text, matrix Matrix, clip *Path, paint Paint, paintMatrix Matrix) bool
}

type textPaint struct {
	paint  Paint
	matrix Matrix
}

func (p *textPaint) memorySize() int64 {
	const baseBytes, stopBytes = 128, 32

	var stops int

	switch gradient := p.paint.Gradient.(type) {
	case *LinearGradient:
		stops = len(gradient.Grad)
	case *RadialGradient:
		stops = len(gradient.Grad)
	}

	return baseBytes + int64(stops)*stopBytes
}

// RenderTextWithPaint forwards an independent paint space without applying the
// Context's view a second time. Unsupported renderers request ordinary replay.
func (c *Context) RenderTextWithPaint(text *Text, matrix Matrix, clip *Path, paint Paint, paintMatrix Matrix) bool {
	if renderer, ok := c.Renderer.(TextPaintRenderer); ok {
		return renderer.RenderTextWithPaint(text, matrix, clip, paint, paintMatrix)
	}

	return false
}

// RenderTextWithPaint records immutable text together with an equivalent paint
// in a shared coordinate system. It does not alter glyph positions, span paints
// or the text-local clip. The paint and its gradient must remain immutable.
func (c *Canvas) RenderTextWithPaint(text *Text, matrix Matrix, clip *Path, paint Paint, paintMatrix Matrix) bool {
	if text == nil || text.Empty() || (clip != nil && clip.Empty()) {
		return true
	}

	if c.layers == nil {
		c.layers = map[int][]layer{}
	}

	if clip != nil {
		clip = clip.Copy()
	}

	c.layers[c.zindex] = append(c.layers[c.zindex], layer{
		path: nil, text: text, img: nil, scene: nil, clip: clip,
		textPaint: &textPaint{paint: paint, matrix: paintMatrix}, m: matrix, style: DefaultStyle,
	})

	return true
}
