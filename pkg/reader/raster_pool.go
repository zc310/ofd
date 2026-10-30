package reader

import (
	"image"
	"sync"
)

// 每个 Reader 最多保留两块空闲页面缓冲，容量合计不超过 64 MiB。
// 超出预算的高 DPI 页面仍可正常渲染，编码后直接释放其缓冲。
const maxRasterBufferBytes = 64 << 20

type rasterBufferPool struct {
	mu      sync.Mutex
	buffers [2]*image.RGBA
	bytes   int
}

func (p *rasterBufferPool) get() *image.RGBA {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, buffer := range p.buffers {
		if buffer != nil {
			p.buffers[i] = nil
			p.bytes -= cap(buffer.Pix)
			return buffer
		}
	}
	return &image.RGBA{}
}

func (p *rasterBufferPool) put(buffer *image.RGBA) {
	size := cap(buffer.Pix)
	if size == 0 || size > maxRasterBufferBytes {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if size > maxRasterBufferBytes-p.bytes {
		return
	}
	for i, cached := range p.buffers {
		if cached == nil {
			p.buffers[i] = buffer
			p.bytes += size
			return
		}
	}
}

func (p *rasterBufferPool) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buffers = [2]*image.RGBA{}
	p.bytes = 0
}
