//go:build js && wasm

package main

import (
	"bytes"
)

const maxWASMRenderBufferCapacity = 8 << 20

func (a *wasmApp) acquireWASMRenderBuffer() *bytes.Buffer {
	buffer, _ := a.renderBuffers.Get().(*bytes.Buffer)
	if buffer == nil {
		buffer = new(bytes.Buffer)
	}
	buffer.Reset()
	if buffer.Cap() == 0 {
		buffer.Grow(64 << 10)
	}
	return buffer
}

func (a *wasmApp) releaseWASMRenderBuffer(buffer *bytes.Buffer) {
	if buffer == nil || buffer.Cap() > maxWASMRenderBufferCapacity {
		return
	}
	buffer.Reset()
	a.renderBuffers.Put(buffer)
}
