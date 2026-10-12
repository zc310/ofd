//go:build !js || !wasm

package canvas

import (
	"io"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/ps"
	"github.com/zc310/canvas/renderers/tex"
)

func extraVectorWriter(format string) (canvas.Writer, bool) {
	switch format {
	case "eps":
		options := ps.DefaultOptions
		options.Format = ps.EncapsulatedPostScript
		return func(w io.Writer, c *canvas.Canvas) error {
			renderer := ps.New(w, c.W, c.H, &options)
			c.RenderTo(renderer)
			return renderer.Close()
		}, true
	case "tex":
		return func(w io.Writer, c *canvas.Canvas) error {
			renderer := tex.New(w, c.W, c.H)
			c.RenderTo(renderer)
			return renderer.Close()
		}, true
	default:
		return nil, false
	}
}
