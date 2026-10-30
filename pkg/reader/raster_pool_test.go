package reader

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func TestRasterBufferPoolLimitsIdleMemory(t *testing.T) {
	var pool rasterBufferPool
	first, second, third := pool.get(), pool.get(), pool.get()
	if first == second || first == third || second == third {
		t.Fatal("并发使用的缓冲必须独立")
	}
	for _, buffer := range []*image.RGBA{first, second, third} {
		buffer.Pix = make([]byte, 4)
		pool.put(buffer)
	}
	if pool.bytes != 8 {
		t.Fatalf("空闲数量未受限，保留 %d 字节", pool.bytes)
	}
	if buffer := pool.get(); buffer != first || pool.bytes != 4 {
		t.Fatal("没有复用并移出空闲缓冲")
	}
	pool.clear()
	if pool.bytes != 0 || pool.buffers != [2]*image.RGBA{} {
		t.Fatal("清理后仍持有空闲缓冲")
	}

	// 按容量计费，即使 Pix 长度为零也不能绕过预算。
	large := &image.RGBA{Pix: make([]byte, 0, maxRasterBufferBytes/2+4)}
	other := &image.RGBA{Pix: make([]byte, 0, maxRasterBufferBytes/2+4)}
	pool.put(large)
	pool.put(other)
	if pool.bytes != cap(large.Pix) || pool.buffers[1] != nil {
		t.Fatal("缓冲容量合计超过预算")
	}
}

func TestRenderPagePoolClearsPreviousPixelsAndReleasesOnClose(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	if _, err := r.RenderPage(t.Context(), 0, RenderOptions{Format: RenderJPG, DPI: 25.4}); err != nil {
		t.Fatal(err)
	}
	buffer := r.rasterBuffers.buffers[0]
	if buffer == nil {
		t.Fatal("编码完成后没有归还 RGBA")
	}
	pixels := &buffer.Pix[0]
	data, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 25.4})
	if err != nil {
		t.Fatal(err)
	}
	if r.rasterBuffers.buffers[0] != buffer || &buffer.Pix[0] != pixels {
		t.Fatal("重复渲染没有复用 RGBA 像素缓冲")
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, alpha := decoded.At(0, 0).RGBA(); alpha != 0 {
		t.Fatal("PNG 留下了上一张 JPG 的白色背景")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if r.rasterBuffers.bytes != 0 || r.rasterBuffers.buffers != [2]*image.RGBA{} {
		t.Fatal("关闭 Reader 后仍持有页面像素")
	}
}

func TestConcurrentRenderPagePoolKeepsOutputsIndependent(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	options := []RenderOptions{
		{DPI: 25.4},
		{DPI: 50.8, Background: color.RGBA{R: 128, A: 128}},
		{DPI: 25.4, Format: RenderJPG},
		{DPI: 50.8, Background: color.White},
	}
	expected := make([][]byte, len(options))
	for i, option := range options {
		r.rasterBuffers.clear()
		data, err := r.RenderPage(t.Context(), 0, option)
		if err != nil {
			t.Fatal(err)
		}
		expected[i] = data
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for round := range 8 {
				index := (worker + round) % len(options)
				data, err := r.RenderPage(t.Context(), 0, options[index])
				if err != nil || !bytes.Equal(data, expected[index]) {
					t.Errorf("并发渲染输出被覆盖：worker=%d options=%d err=%v", worker, index, err)
					return
				}
			}
		})
	}
	wg.Wait()
	for i, option := range options {
		data, err := r.RenderPage(t.Context(), 0, option)
		if err != nil || !bytes.Equal(data, expected[i]) {
			t.Fatalf("后续渲染改写了已返回的编码字节：options=%d err=%v", i, err)
		}
	}
}

type cancelBorrowedRasterContext struct {
	context.Context //nolint:containedctx // 测试包装上下文，在借出缓冲后触发真正的取消。

	pool   *rasterBufferPool
	cancel context.CancelFunc
}

func (c cancelBorrowedRasterContext) Err() error {
	c.pool.mu.Lock()
	borrowed := c.pool.bytes == 0
	c.pool.mu.Unlock()
	if borrowed {
		c.cancel()
	}
	return c.Context.Err()
}

func TestRenderPagePoolReturnsBufferOnCancellation(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	options := RenderOptions{DPI: 25.4}
	if _, err := r.RenderPage(t.Context(), 0, options); err != nil {
		t.Fatal(err)
	}
	buffer := r.rasterBuffers.buffers[0]
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observed := cancelBorrowedRasterContext{Context: ctx, pool: &r.rasterBuffers, cancel: cancel}
	if data, err := r.RenderPage(observed, 0, options); !errors.Is(err, context.Canceled) || len(data) != 0 {
		t.Fatalf("栅格化后取消：输出 %d 字节，错误 %v", len(data), err)
	}
	if r.rasterBuffers.buffers[0] != buffer {
		t.Fatal("取消后没有归还借出的 RGBA")
	}
	if _, err := r.RenderPage(t.Context(), 0, options); err != nil {
		t.Fatalf("取消后无法再次渲染：%v", err)
	}
}

// 对比阅读器重复渲染 A4 页面时，每次分配与复用 RGBA 的开销。
func BenchmarkRenderPageRasterBuffer(b *testing.B) {
	data, err := creator.Marshal(creator.Document{ID: "raster-pool", PageSize: creator.A4, Pages: []creator.Page{{}}})
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"fresh", "pooled"} {
		b.Run(name, func(b *testing.B) {
			r, err := Open(data)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = r.Close() })
			options := RenderOptions{DPI: 150}
			if _, err := r.RenderPage(b.Context(), 0, options); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if name == "fresh" {
					r.rasterBuffers.clear()
				}
				if _, err := r.RenderPage(b.Context(), 0, options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
