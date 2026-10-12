//go:build js && wasm

package canvas

import "github.com/tdewolff/canvas"

func extraVectorWriter(string) (canvas.Writer, bool) {
	return nil, false
}
