package pdf

import "github.com/zc310/ofd/internal/thirdparty/canvas" //nolint:depguard // Native clipping consumes canvas geometry and styles.

// RenderClippedPath retains native curves and strokes under a PDF clipping
// region. The path-local clip follows the same transform as the source path.
func (r *PDF) RenderClippedPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix, clip *canvas.Path) {
	if path == nil || path.Empty() || clip == nil || clip.Empty() || matrix.Det() == 0 {
		return
	}

	state := r.w.pushState()
	defer r.w.popState(state)

	_, _ = r.w.WriteString(" ")
	_, _ = r.w.WriteString(clip.Copy().Transform(matrix).ToPDF())
	_, _ = r.w.WriteString(" W n ")
	r.RenderPath(path, style, matrix)
}
