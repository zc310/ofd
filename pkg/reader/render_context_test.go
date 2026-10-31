package reader

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"testing"
	"time"
)

type renderTestWriter func([]byte) (int, error)

func (w renderTestWriter) Write(data []byte) (int, error) { return w(data) }

type cancelRenderBackground struct {
	cancel context.CancelFunc
}

func (c cancelRenderBackground) RGBA() (uint32, uint32, uint32, uint32) {
	c.cancel()
	return color.White.RGBA()
}

type observedCancelBackground struct {
	ctx              context.Context
	callsAfterCancel int
}

func (c *observedCancelBackground) RGBA() (uint32, uint32, uint32, uint32) {
	if c.ctx.Err() != nil {
		c.callsAfterCancel++
	}
	return color.White.RGBA()
}

func TestRenderContextRejectsCanceledAndExpiredRequestsWithoutOutput(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := context.Canceled
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want = context.DeadlineExceeded
		}
		cancel()
		if data, err := renderPageBytes(ctx, r, 0, RenderOptions{}); !errors.Is(err, want) || len(data) != 0 {
			t.Fatalf("已取消页面渲染 = %d 字节, %v，期望 %v", len(data), err, want)
		}
		for _, indices := range [][]int{nil, {0, 0}} {
			if data, err := renderPagesBytes(ctx, r, indices, RenderOptions{}); !errors.Is(err, want) || len(data) != 0 {
				t.Fatalf("已取消批量渲染 = %d 页, %v，期望 %v", len(data), err, want)
			}
		}
		if data, err := renderPDFBytes(ctx, r, []int{0}, RenderOptions{}); !errors.Is(err, want) || len(data) != 0 {
			t.Fatalf("已取消 PDF 内存导出 = %d 字节, %v，期望 %v", len(data), err, want)
		}
		if links, err := r.pdfPageLinks(ctx, []int{0}); err != want || len(links) != 0 {
			t.Fatalf("已取消 PDF 链接收集 = %d 页, %v，期望 %v", len(links), err, want)
		}
		var output bytes.Buffer
		if err := r.RenderPDF(ctx, &output, []int{0}, RenderOptions{}); !errors.Is(err, want) || output.Len() != 0 {
			t.Fatalf("已取消 PDF 渲染 = %d 字节, %v，期望 %v", output.Len(), err, want)
		}
	}
}

func TestRenderBatchAndPDFPropagateCancellationDuringDrawing(t *testing.T) {
	for _, format := range []string{"pages", "pdf"} {
		t.Run(format, func(t *testing.T) {
			r := pdfSizeTestReader(t, 20, 20)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			options := RenderOptions{DPI: 24, Background: cancelRenderBackground{cancel: cancel}}
			var err error
			if format == "pages" {
				var data [][]byte
				data, err = renderPagesBytes(ctx, r, []int{0, 0}, options)
				if len(data) != 0 {
					t.Fatal("取消后返回了部分页面")
				}
			} else {
				var data []byte
				data, err = renderPDFBytes(ctx, r, []int{0, 0}, options)
				if len(data) != 0 {
					t.Fatal("取消后返回了部分 PDF")
				}
			}
			if err != context.Canceled || len(r.renderDocUsers) != 0 {
				t.Fatalf("取消未传入绘制或未释放资源: err=%v users=%d", err, len(r.renderDocUsers))
			}
			if _, err := renderPagesBytes(t.Context(), r, []int{0, 0}, RenderOptions{DPI: 24}); err != nil {
				t.Fatalf("取消后无法再次批量渲染: %v", err)
			}
		})
	}
}

func TestRenderPageStopsAfterDrawingAndReleasesResources(t *testing.T) {
	for _, format := range []RenderFormat{RenderPNG, RenderJPG, RenderSVG} {
		t.Run(string(format), func(t *testing.T) {
			r := pdfSizeTestReader(t, 20, 20)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			data, err := renderPageBytes(ctx, r, 0, RenderOptions{
				Format: format, Background: cancelRenderBackground{cancel: cancel},
			})
			if err != context.Canceled || len(data) != 0 {
				t.Fatalf("绘制中取消 = %d 字节, %v", len(data), err)
			}
			if len(r.renderDocUsers) != 0 {
				t.Fatal("取消后仍持有渲染视图")
			}
			if _, err := renderPageBytes(t.Context(), r, 0, RenderOptions{DPI: 24}); err != nil {
				t.Fatalf("取消后无法再次渲染: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRenderPageWritesToReusableOutputAndPropagatesWriterErrors(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	var output bytes.Buffer
	options := RenderOptions{DPI: 24}
	if err := r.RenderPage(t.Context(), &output, 0, options); err != nil {
		t.Fatal(err)
	}
	first := bytes.Clone(output.Bytes())
	if len(first) == 0 {
		t.Fatal("页面没有写入输出")
	}
	output.Reset()
	if err := r.RenderPage(t.Context(), &output, 0, options); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), first) {
		t.Fatal("复用 writer 后渲染输出发生变化")
	}

	want := errors.New("输出失败")
	writer := renderTestWriter(func([]byte) (int, error) { return 0, want })
	if err := r.RenderPage(t.Context(), writer, 0, options); !errors.Is(err, want) {
		t.Fatalf("页面 writer 错误未传播: %v", err)
	}
}

func TestRenderPagesConsumesAndReusesPageBuffer(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	var firstAddress *byte
	var pages [][]byte
	calls := 0
	err := r.RenderPages(t.Context(), []int{0, 0}, RenderOptions{DPI: 24}, func(_ context.Context, position, pageIndex int, data []byte) error {
		if position != calls || pageIndex != 0 {
			return errors.New("页面回调顺序错误")
		}
		if len(data) == 0 {
			return errors.New("页面输出为空")
		}
		if calls == 0 {
			firstAddress = &data[0]
		} else if &data[0] != firstAddress {
			return errors.New("页面输出缓冲没有复用")
		}
		pages = append(pages, bytes.Clone(data))
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(pages) != 2 || !bytes.Equal(pages[0], pages[1]) {
		t.Fatalf("页面回调结果不正确: calls=%d pages=%d", calls, len(pages))
	}
}

func TestRenderPagesStopsWhenConsumerReturnsError(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	want := errors.New("消费失败")
	calls := 0
	err := r.RenderPages(t.Context(), []int{0, 0}, RenderOptions{DPI: 24}, func(context.Context, int, int, []byte) error {
		calls++
		return want
	})
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("消费错误未停止后续渲染: err=%v calls=%d", err, calls)
	}
}

func TestRenderPagesValidatesAllIndicesBeforeConsuming(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	calls := 0
	err := r.RenderPages(t.Context(), []int{0, -1}, RenderOptions{DPI: 24}, func(context.Context, int, int, []byte) error {
		calls++
		return nil
	})
	if err == nil || calls != 0 {
		t.Fatalf("无效批次产生了部分回调: err=%v calls=%d", err, calls)
	}
}

func TestRenderPagesPropagatesCancellationFromConsumer(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	err := r.RenderPages(ctx, []int{0, 0}, RenderOptions{DPI: 24}, func(callbackCtx context.Context, _, _ int, data []byte) error {
		calls++
		if callbackCtx != ctx {
			return errors.New("回调没有收到渲染 context")
		}
		if len(data) == 0 {
			return errors.New("页面输出为空")
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("回调取消未阻止后续页面: err=%v calls=%d", err, calls)
	}
}

func TestRenderPagesAcceptsMoreThan64Pages(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	indices := make([]int, 65)
	calls := 0
	err := r.RenderPages(t.Context(), indices, RenderOptions{DPI: 12}, func(_ context.Context, position, pageIndex int, data []byte) error {
		if position != calls || pageIndex != 0 || len(data) == 0 {
			return errors.New("页面回调结果错误")
		}
		calls++
		return nil
	})
	if err != nil || calls != len(indices) {
		t.Fatalf("超过64页的批量渲染失败: err=%v calls=%d", err, calls)
	}
}

func TestRenderPDFStopsOnCancellationDuringOutput(t *testing.T) {
	r := pdfSizeTestReader(t, 20, 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	background := &observedCancelBackground{ctx: ctx}
	writes := 0
	output := renderTestWriter(func(data []byte) (int, error) {
		writes++
		cancel()
		return len(data), nil
	})
	if err := r.RenderPDF(ctx, output, []int{0, 0, 0}, RenderOptions{Background: background}); !errors.Is(err, context.Canceled) {
		t.Fatalf("输出中取消 = %v", err)
	}
	if writes != 1 || len(r.renderDocUsers) != 0 {
		t.Fatalf("取消后仍写出或持有视图: writes=%d users=%d", writes, len(r.renderDocUsers))
	}
	if background.callsAfterCancel != 0 {
		t.Fatalf("取消后仍处理后续页背景 %d 次", background.callsAfterCancel)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRenderPDFStopsBeforeNextPageWhenBackendDefersWriteError(t *testing.T) {
	for _, want := range []error{errors.New("输出失败"), io.ErrShortWrite} {
		r := pdfSizeTestReader(t, 20, 20)
		writes := 0
		output := renderTestWriter(func([]byte) (int, error) {
			writes++
			if want == io.ErrShortWrite {
				return 0, nil
			}
			return 0, want
		})
		// 权限预检会先验证全部页码；重复有效页仍应在首次输出失败后停止写入。
		err := r.RenderPDF(t.Context(), output, []int{0, 0}, RenderOptions{})
		if !errors.Is(err, want) || writes != 1 {
			t.Fatalf("输出错误未及时终止: err=%v writes=%d，期望 %v", err, writes, want)
		}
	}
}
