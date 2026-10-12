//go:build js && wasm

package canvas

import "github.com/zc310/canvas"

func extraVectorWriter(string) (canvas.Writer, bool) {
	return nil, false
}
