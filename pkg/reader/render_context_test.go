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
		if data, err := r.RenderPage(ctx, 0, RenderOptions{}); err != want || len(data) != 0 {
			t.Fatalf("已取消页面渲染 = %d 字节, %v，期望 %v", len(data), err, want)
		}
		for _, indices := range [][]int{nil, {0, 0}} {
			if data, err := r.RenderPages(ctx, indices, RenderOptions{}); err != want || len(data) != 0 {
				t.Fatalf("已取消批量渲染 = %d 页, %v，期望 %v", len(data), err, want)
			}
		}
		if data, err := r.RenderPDF(ctx, []int{0}, RenderOptions{}); err != want || len(data) != 0 {
			t.Fatalf("已取消 PDF 内存导出 = %d 字节, %v，期望 %v", len(data), err, want)
		}
		if links, err := r.pdfPageLinks(ctx, []int{0}); err != want || len(links) != 0 {
			t.Fatalf("已取消 PDF 链接收集 = %d 页, %v，期望 %v", len(links), err, want)
		}
		var output bytes.Buffer
		if err := r.RenderPDFTo(ctx, &output, []int{0}, RenderOptions{}); err != want || output.Len() != 0 {
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
				data, err = r.RenderPages(ctx, []int{0, 0}, options)
				if len(data) != 0 {
					t.Fatal("取消后返回了部分页面")
				}
			} else {
				var data []byte
				data, err = r.RenderPDF(ctx, []int{0, 0}, options)
				if len(data) != 0 {
					t.Fatal("取消后返回了部分 PDF")
				}
			}
			if err != context.Canceled || len(r.renderDocUsers) != 0 {
				t.Fatalf("取消未传入绘制或未释放资源: err=%v users=%d", err, len(r.renderDocUsers))
			}
			if _, err := r.RenderPages(t.Context(), []int{0, 0}, RenderOptions{DPI: 24}); err != nil {
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
			data, err := r.RenderPage(ctx, 0, RenderOptions{
				Format: format, Background: cancelRenderBackground{cancel: cancel},
			})
			if err != context.Canceled || len(data) != 0 {
				t.Fatalf("绘制中取消 = %d 字节, %v", len(data), err)
			}
			if len(r.renderDocUsers) != 0 {
				t.Fatal("取消后仍持有渲染视图")
			}
			if _, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 24}); err != nil {
				t.Fatalf("取消后无法再次渲染: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRenderPDFToStopsOnCancellationDuringOutput(t *testing.T) {
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
	if err := r.RenderPDFTo(ctx, output, []int{0, 0, 0}, RenderOptions{Background: background}); err != context.Canceled {
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

func TestRenderPDFToStopsBeforeNextPageWhenBackendDefersWriteError(t *testing.T) {
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
		err := r.RenderPDFTo(t.Context(), output, []int{0, 0}, RenderOptions{})
		if !errors.Is(err, want) || writes != 1 {
			t.Fatalf("输出错误未及时终止: err=%v writes=%d，期望 %v", err, writes, want)
		}
	}
}
