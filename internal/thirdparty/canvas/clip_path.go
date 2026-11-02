package canvas

import "slices"

// ClippedPathRenderer preserves a path and its style under a path-local clip.
// Clips use NonZero winding; settle EvenOdd clip paths before passing them in.
type ClippedPathRenderer interface {
	RenderClippedPath(path *Path, style Style, matrix Matrix, clip *Path)
}

// RenderClippedPath forwards already positioned geometry without applying the
// Context view again, just like RenderPath. Unsupported renderers use outlines.
func (c *Context) RenderClippedPath(path *Path, style Style, matrix Matrix, clip *Path) {
	if renderer, ok := c.Renderer.(ClippedPathRenderer); ok {
		renderer.RenderClippedPath(path, style, matrix, clip)
	} else {
		renderClippedPath(c.Renderer, path, style, matrix, clip)
	}
}

// RenderClippedPath records copies of the path, clip and dash array. Paints,
// cappers and joiners must remain immutable, as for ordinary recorded paths.
func (c *Canvas) RenderClippedPath(path *Path, style Style, matrix Matrix, clip *Path) {
	if path == nil || path.Empty() || clip == nil || clip.Empty() {
		return
	}

	if c.layers == nil {
		c.layers = map[int][]layer{}
	}

	style.Dashes = slices.Clone(style.Dashes)
	c.layers[c.zindex] = append(c.layers[c.zindex], layer{
		path: path.Copy(), text: nil, img: nil, scene: nil, clip: clip.Copy(), textPaint: nil,
		m: matrix, style: style,
	})
}

// renderClippedPath keeps the former outline-intersection behavior for renderers
// without native clipping. Both geometry and clip remain in the same space.
func renderClippedPath(renderer Renderer, path *Path, style Style, matrix Matrix, clip *Path) {
	if path == nil || path.Empty() || clip == nil || clip.Empty() || matrix.Det() == 0 {
		return
	}

	var noPaint Paint

	if style.HasFill() {
		fill := closedTextFill(path, style.FillRule).And(clip)
		fillStyle := style
		fillStyle.Stroke = noPaint
		fillStyle.FillRule = NonZero

		if !fill.Empty() {
			renderer.RenderPath(fill, fillStyle, matrix)
		}
	}

	if style.HasStroke() {
		stroke := clippedPathStroke(path, style, clip)
		style.Fill, style.Stroke, style.FillRule = style.Stroke, noPaint, NonZero

		if !stroke.Empty() {
			renderer.RenderPath(stroke, style, matrix)
		}
	}
}

func clippedPathStroke(path *Path, style Style, clip *Path) *Path {
	if style.IsDashed() {
		offset, dashes := ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
		path = path.Dash(offset, dashes...)
	}

	return path.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, Tolerance).And(clip)
}
